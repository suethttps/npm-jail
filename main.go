// npm-jail roda comandos npm dentro de um sandbox bubblewrap (bwrap).
//
// Modelo de seguranca (padrao, sem flags):
//   - $HOME vira um tmpfs vazio (efemero): .ssh, .aws, .gnupg, tokens,
//     historico de shell etc. simplesmente NAO existem dentro do jail.
//   - Apenas o diretorio do projeto (cwd) e montado read-write.
//   - O sistema (/usr, /etc, /opt) entra read-only.
//   - O toolchain do Node (node/npm/npx) entra read-only.
//   - O cache do npm (~/.npm) entra read-write para reaproveitar downloads.
//   - ~/.npmrc entra read-only (so e montado se existir).
//   - PID/UTS/IPC/cgroup ficam isolados em namespaces proprios.
//   - Rede e compartilhada com o host por padrao (npm install precisa);
//     use --no-net para isolar tambem a rede.
//
// Config por projeto:
//   Um arquivo .npm-jail (JSON) no diretorio atual e lido automaticamente.
//   As flags da CLI tem prioridade sobre ele. Veja "npm-jail --init".
//
// Uso:
//   npm-jail [flags do npm-jail] <argumentos do npm>
//   npm-jail install express
//   npm-jail --no-net run build
//   npm-jail --rw ./dist ci
//   npm-jail --dry-run install        # so mostra a linha do bwrap
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const configName = ".npm-jail"

const usage = `npm-jail - roda npm dentro de um sandbox bubblewrap

USO:
    npm-jail [flags do npm-jail] <argumentos do npm>

EXEMPLOS:
    npm-jail install express
    npm-jail --no-net run build
    npm-jail --rw ./out --ro ~/.config/some ci
    npm-jail --dry-run install
    npm-jail --init                    # gera um .npm-jail de exemplo

FLAGS DO npm-jail (devem vir ANTES dos argumentos do npm):
    --no-net           Isola a rede (--unshare-net). npm install que baixa
                       pacotes vai falhar; bom para builds offline.
    --net              Forca a rede LIGADA (sobrepoe no_net do .npm-jail).
    --rw PATH          Monta PATH adicional read-write (pode repetir).
    --ro PATH          Monta PATH adicional read-only (pode repetir).
    --allow-global     Deixa o toolchain do Node read-write (permite npm i -g).
    --share-home       NAO usa tmpfs no $HOME (expoe o home real). Inseguro;
                       use so para depurar.
    --no-config        Ignora o arquivo .npm-jail do projeto.
    --init             Cria um .npm-jail de exemplo no diretorio atual e sai.
    --verbose, -v      Imprime a linha completa do bwrap antes de executar.
    --dry-run          Imprime a linha do bwrap e sai (nao executa).
    --help, -h         Mostra esta ajuda.

ARQUIVO .npm-jail (JSON, opcional, no diretorio do projeto):
    {
      "no_net": false,
      "allow_global": false,
      "rw": ["./out"],
      "ro": ["~/.config/algum"]
    }
    Tudo que vier depois da primeira coisa que nao for flag conhecida (ou
    depois de "--") e repassado intacto para o npm.
`

// config e o estado final ja resolvido (arquivo + CLI).
type config struct {
	noNet       bool
	allowGlobal bool
	shareHome   bool
	verbose     bool
	dryRun      bool
	rwExtra     []string
	roExtra     []string
	npmArgs     []string
}

// fileConfig e o formato do arquivo .npm-jail.
type fileConfig struct {
	NoNet       bool     `json:"no_net"`
	AllowGlobal bool     `json:"allow_global"`
	RW          []string `json:"rw"`
	RO          []string `json:"ro"`
}

// cliFlags guarda o que veio da linha de comando. Booleanos sao ponteiros
// para distinguir "nao informado" de "informado como false", permitindo que
// a CLI sobreponha o arquivo de forma previsivel.
type cliFlags struct {
	noNet       *bool
	allowGlobal *bool
	shareHome   *bool
	verbose     bool
	dryRun      bool
	noConfig    bool
	doInit      bool
	rw          []string
	ro          []string
	npmArgs     []string
}

func main() {
	cli, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: nao consegui determinar o diretorio atual")
		os.Exit(1)
	}

	if cli.doInit {
		if err := writeSampleConfig(cwd); err != nil {
			fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
			os.Exit(1)
		}
		fmt.Println("npm-jail: criado " + filepath.Join(cwd, configName))
		return
	}

	if len(cli.npmArgs) == 0 {
		fmt.Print(usage)
		return
	}

	cfg, err := resolveConfig(cwd, cli)
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
		os.Exit(1)
	}

	args, err := buildBwrapArgs(cwd, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
		os.Exit(1)
	}

	if cfg.verbose || cfg.dryRun {
		fmt.Fprintln(os.Stderr, "bwrap "+shellQuote(args))
	}
	if cfg.dryRun {
		return
	}

	cmd := exec.Command("bwrap", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "npm-jail: falha ao executar bwrap: "+err.Error())
		os.Exit(1)
	}
}

func parseArgs(in []string) (cliFlags, error) {
	var c cliFlags
	bTrue, bFalse := true, false
	for i := 0; i < len(in); i++ {
		a := in[i]
		switch a {
		case "--help", "-h":
			fmt.Print(usage)
			os.Exit(0)
		case "--init":
			c.doInit = true
		case "--no-config":
			c.noConfig = true
		case "--no-net":
			c.noNet = &bTrue
		case "--net":
			c.noNet = &bFalse
		case "--allow-global":
			c.allowGlobal = &bTrue
		case "--share-home":
			c.shareHome = &bTrue
		case "--verbose", "-v":
			c.verbose = true
		case "--dry-run":
			c.dryRun = true
		case "--rw":
			i++
			if i >= len(in) {
				return c, fmt.Errorf("--rw exige um PATH")
			}
			c.rw = append(c.rw, in[i])
		case "--ro":
			i++
			if i >= len(in) {
				return c, fmt.Errorf("--ro exige um PATH")
			}
			c.ro = append(c.ro, in[i])
		case "--":
			c.npmArgs = append(c.npmArgs, in[i+1:]...)
			return c, nil
		default:
			// Primeira coisa nao reconhecida: dali em diante e tudo do npm.
			c.npmArgs = append(c.npmArgs, in[i:]...)
			return c, nil
		}
	}
	return c, nil
}

// resolveConfig junta o arquivo .npm-jail (defaults) com as flags da CLI
// (prioridade). Listas rw/ro sao unidas; booleanos da CLI sobrepoem.
func resolveConfig(cwd string, cli cliFlags) (config, error) {
	var fc fileConfig
	if !cli.noConfig {
		loaded, err := loadConfig(cwd)
		if err != nil {
			return config{}, err
		}
		if loaded != nil {
			fc = *loaded
		}
	}

	cfg := config{
		noNet:       fc.NoNet,
		allowGlobal: fc.AllowGlobal,
		verbose:     cli.verbose,
		dryRun:      cli.dryRun,
		rwExtra:     append(append([]string{}, fc.RW...), cli.rw...),
		roExtra:     append(append([]string{}, fc.RO...), cli.ro...),
		npmArgs:     cli.npmArgs,
	}
	if cli.noNet != nil {
		cfg.noNet = *cli.noNet
	}
	if cli.allowGlobal != nil {
		cfg.allowGlobal = *cli.allowGlobal
	}
	if cli.shareHome != nil {
		cfg.shareHome = *cli.shareHome
	}
	return cfg, nil
}

// loadConfig le .npm-jail do diretorio do projeto. Retorna nil se nao existir.
func loadConfig(cwd string) (*fileConfig, error) {
	path := filepath.Join(cwd, configName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nao consegui ler %s: %w", configName, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("%s invalido (JSON): %w", configName, err)
	}
	return &fc, nil
}

func writeSampleConfig(cwd string) error {
	path := filepath.Join(cwd, configName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s ja existe", configName)
	}
	sample := fileConfig{
		NoNet:       false,
		AllowGlobal: false,
		RW:          []string{},
		RO:          []string{},
	}
	data, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func buildBwrapArgs(cwd string, cfg config) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("nao consegui determinar o $HOME")
	}

	toolchain, binDir, err := resolveNodeToolchain()
	if err != nil {
		return nil, err
	}

	var a []string
	add := func(xs ...string) { a = append(a, xs...) }

	// Isolamento de namespaces e seguranca basica.
	add("--die-with-parent")
	add("--unshare-pid", "--unshare-uts", "--unshare-ipc", "--unshare-cgroup-try")
	if cfg.noNet {
		add("--unshare-net")
	}

	// Raiz do sistema, read-only. /usr + recriacao dos symlinks usr-merge.
	add("--ro-bind", "/usr", "/usr")
	for _, link := range []string{"/bin", "/sbin", "/lib", "/lib64", "/lib32"} {
		addRootEntry(&a, link)
	}
	for _, dir := range []string{"/etc", "/opt"} {
		if isDir(dir) {
			add("--ro-bind", dir, dir)
		}
	}

	// Pseudo-filesystems e dirs efemeros.
	add("--proc", "/proc")
	add("--dev", "/dev")
	add("--tmpfs", "/tmp")
	add("--tmpfs", "/run")

	// Com rede compartilhada precisamos do resolv.conf funcionando.
	if !cfg.noNet {
		addResolvConf(&a)
	}

	// $HOME efemero (esconde tudo) e depois remonta so o necessario.
	if !cfg.shareHome {
		add("--tmpfs", home)
	}

	// Toolchain do Node (node/npm/npx). Vem DEPOIS do tmpfs do home,
	// pois normalmente vive dentro do $HOME (ex.: mise).
	if cfg.allowGlobal {
		add("--bind", toolchain, toolchain)
	} else {
		add("--ro-bind", toolchain, toolchain)
	}

	// Cache do npm (read-write) e .npmrc (read-only, se existir).
	cache := npmCacheDir(home)
	add("--bind-try", cache, cache)
	npmrc := filepath.Join(home, ".npmrc")
	if fileExists(npmrc) {
		add("--ro-bind", npmrc, npmrc)
	}

	// Diretorio do projeto: read-write.
	add("--bind", cwd, cwd)

	// Montagens extras pedidas pelo usuario (arquivo + CLI).
	for _, p := range cfg.roExtra {
		abs := mustAbs(home, p)
		add("--ro-bind-try", abs, abs)
	}
	for _, p := range cfg.rwExtra {
		abs := mustAbs(home, p)
		add("--bind-try", abs, abs)
	}

	// Ambiente: herda o do host, mas fixa HOME, PATH e cwd.
	path := binDir + ":/usr/bin:/usr/local/bin"
	add("--setenv", "HOME", home)
	add("--setenv", "PATH", path)
	add("--chdir", cwd)

	// Comando final.
	add("--", "npm")
	add(cfg.npmArgs...)
	return a, nil
}

// addResolvConf garante DNS dentro do jail quando a rede e compartilhada.
//
// Em distros com systemd-resolved, /etc/resolv.conf e um symlink para algo
// dentro de /run (que zeramos com tmpfs). Como /etc esta read-only, nao da
// para criar arquivo la; entao montamos o arquivo real no DESTINO do symlink,
// que cai em /run (tmpfs gravavel), fazendo o symlink voltar a resolver.
// Se resolv.conf for um arquivo comum, o bind read-only de /etc ja resolve.
func addResolvConf(a *[]string) {
	real, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil {
		return
	}
	fi, err := os.Lstat("/etc/resolv.conf")
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return // arquivo comum: ja veio no --ro-bind /etc
	}
	target, err := os.Readlink("/etc/resolv.conf")
	if err != nil {
		return
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join("/etc", target)
	}
	*a = append(*a, "--ro-bind", real, target)
}

// addRootEntry replica uma entrada de raiz (/bin, /lib, ...): se for symlink
// (layout usr-merge), recria o symlink; se for diretorio real, monta read-only.
func addRootEntry(a *[]string, p string) {
	fi, err := os.Lstat(p)
	if err != nil {
		return // nao existe nesta distro
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(p)
		if err == nil {
			*a = append(*a, "--symlink", target, p)
		}
		return
	}
	if fi.IsDir() {
		*a = append(*a, "--ro-bind", p, p)
	}
}

// resolveNodeToolchain acha a raiz do toolchain do Node a partir do binario
// node no PATH. Retorna (dir do toolchain, dir dos binarios).
//
// Ex.: /home/u/.local/share/mise/installs/node/25.8.0/bin/node
//   -> toolchain = /home/u/.local/share/mise/installs/node/25.8.0
//   -> binDir    = /home/u/.local/share/mise/installs/node/25.8.0/bin
func resolveNodeToolchain() (string, string, error) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return "", "", fmt.Errorf("node nao encontrado no PATH")
	}
	real, err := filepath.EvalSymlinks(nodePath)
	if err != nil {
		return "", "", fmt.Errorf("nao consegui resolver o caminho do node: %w", err)
	}
	binDir := filepath.Dir(real)      // .../bin
	toolchain := filepath.Dir(binDir) // .../<versao>
	return toolchain, binDir, nil
}

func npmCacheDir(home string) string {
	if c := os.Getenv("npm_config_cache"); c != "" {
		return c
	}
	return filepath.Join(home, ".npm")
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mustAbs(home, p string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// shellQuote so serve para imprimir a linha do bwrap de forma legivel.
func shellQuote(args []string) string {
	var b strings.Builder
	for i, s := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if s == "" || strings.ContainsAny(s, " \t\n\"'\\$") {
			b.WriteString("'" + strings.ReplaceAll(s, "'", `'\''`) + "'")
		} else {
			b.WriteString(s)
		}
	}
	return b.String()
}
