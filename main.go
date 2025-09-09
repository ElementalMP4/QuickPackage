package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cp "github.com/otiai10/copy"
)

type FileEntry struct {
	File string `json:"file"`
	From string `json:"from"`
}

type Config struct {
	AppName         string      `json:"app_name"`
	BuildFiles      []string    `json:"build_files"`
	InstallFiles    []FileEntry `json:"install_files"`
	BuildScript     string      `json:"build_script,omitempty"`
	InstallScript   string      `json:"install_script,omitempty"`
	UninstallScript string      `json:"uninstall_script,omitempty"`
	Systemd         bool        `json:"systemd"`
	Exec            string      `json:"exec,omitempty"`
}

func (c Config) GetBuildScript() string {
	return fmt.Sprintf(".qp/%s", c.BuildScript)
}

func (c Config) GetInstallScript() string {
	return fmt.Sprintf(".qp/%s", c.InstallScript)
}

func (c Config) GetUninstallScript() string {
	return fmt.Sprintf(".qp/%s", c.UninstallScript)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: quickpackage [build|install|uninstall] [--config <path>]")
	}

	action := os.Args[1]
	args := os.Args[2:]

	configPath := ".qp/config.json"
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			i++
		}
	}

	if action != "build" && action != "install" && action != "uninstall" {
		log.Fatalf("Unknown action %q. Must be one of: build, install, uninstall", action)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	validateConfig(cfg)

	switch action {
	case "build":
		doBuild(cfg)
	case "install":
		doBuild(cfg)
		doInstall(cfg)
	case "uninstall":
		doUninstall(cfg)
	}
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateConfig(cfg *Config) {
	if cfg.AppName == "" {
		log.Fatal("config: app_name is required")
	}
	if len(cfg.InstallFiles) == 0 {
		log.Fatal("config: install_files must have at least one entry")
	}
	if cfg.Systemd && cfg.Exec == "" {
		log.Fatal("config: exec command required when systemd=true")
	}
}

func doBuild(cfg *Config) {
	buildDir, err := os.MkdirTemp("/tmp", "qp_build_"+cfg.AppName+"_")
	if err != nil {
		log.Fatalf("Failed to create build temp dir: %v", err)
	}

	log.Printf("Build directory: %s", buildDir)

	for _, pattern := range cfg.BuildFiles {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.Fatalf("Invalid glob pattern %q: %v", pattern, err)
		}
		if len(matches) == 0 {
			log.Printf("Warning: no build files matched pattern %q", pattern)
		}

		baseDir := getGlobBase(pattern)

		for _, src := range matches {
			if err := copyPreserveRel(src, baseDir, buildDir); err != nil {
				log.Fatalf("%v", err)
			}
		}
	}

	if cfg.BuildScript != "" {
		scriptPath := filepath.Join(buildDir, filepath.Base(cfg.BuildScript))
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			err = copyFileOrDir(cfg.GetBuildScript(), scriptPath)
			if err != nil {
				log.Fatalf("Failed to copy build script %s: %v", cfg.BuildScript, err)
			}
		}
		log.Printf("Running build script: %s", scriptPath)
		cmd := exec.Command("/bin/bash", scriptPath)
		cmd.Dir = buildDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Build script failed: %v", err)
		}
	} else {
		log.Println("No build script specified, skipping build step")
	}

	log.Printf("Build complete!")
}

func getGlobBase(pattern string) string {
	base := pattern
	for strings.Contains(base, "*") || strings.Contains(base, "?") || strings.Contains(base, "[") {
		base = filepath.Dir(base)
	}
	return base
}

func copyPreserveRel(src, baseDir, dstRoot string) error {
	relPath, err := filepath.Rel(baseDir, src)
	if err != nil {
		return fmt.Errorf("failed to compute relative path for %s: %w", src, err)
	}

	dst := filepath.Join(dstRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create directories for %s: %w", dst, err)
	}

	if err := copyFileOrDir(src, dst); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", src, dst, err)
	}

	log.Printf("Copied %s -> %s", src, dst)
	return nil
}

func doInstall(cfg *Config) {
	installDir := filepath.Join("/opt/qp_apps", cfg.AppName)
	log.Printf("Installing to %s", installDir)

	if err := os.MkdirAll(installDir, 0755); err != nil {
		log.Fatalf("Failed to create install dir: %v", err)
	}

	buildDir, _ := findTempBuildDir(cfg.AppName)

	for _, entry := range cfg.InstallFiles {
		var srcPath, baseDir string
		switch entry.From {
		case "cwd":
			srcPath = entry.File
			baseDir = "." // keep relative path as-is
		case "build":
			if buildDir == "" {
				log.Fatalf("Build directory unknown, but install file %q is marked from build", entry.File)
			}
			srcPath = filepath.Join(buildDir, entry.File)
			baseDir = buildDir
		default:
			log.Fatalf("Unknown 'from' value %q for install file %q", entry.From, entry.File)
		}

		if !exists(srcPath) {
			log.Fatalf("Install source file %s does not exist", srcPath)
		}

		if err := copyPreserveRel(srcPath, baseDir, installDir); err != nil {
			log.Fatalf("%v", err)
		}
	}

	if cfg.InstallScript != "" {
		scriptPath := filepath.Join(installDir, filepath.Base(cfg.InstallScript))
		if !exists(scriptPath) {
			err := copyFileOrDir(cfg.GetInstallScript(), scriptPath)
			if err != nil {
				log.Fatalf("Failed to copy install script %s: %v", cfg.InstallScript, err)
			}
		}
		log.Printf("Running install script: %s", scriptPath)
		runScript(scriptPath, installDir)
	} else {
		log.Println("No install script specified, skipping install script step")
	}

	if cfg.Systemd {
		err := installSystemdUnit(cfg)
		if err != nil {
			log.Fatalf("Failed to install systemd unit: %v", err)
		}

		serviceName := cfg.AppName + ".service"
		cmdCheck := exec.Command("systemctl", "is-active", "--quiet", serviceName)
		if err := cmdCheck.Run(); err == nil {
			log.Printf("Restarting active systemd service %s", serviceName)
			cmdRestart := exec.Command("systemctl", "restart", serviceName)
			cmdRestart.Stdout = os.Stdout
			cmdRestart.Stderr = os.Stderr
			if err := cmdRestart.Run(); err != nil {
				log.Fatalf("Failed to restart systemd service %s: %v", serviceName, err)
			}
		} else {
			log.Printf("Systemd service %s is not active, no restart needed", serviceName)
		}
	}

	log.Printf("Install completed")
}

func doUninstall(cfg *Config) {
	installDir := filepath.Join("/opt/qp_apps", cfg.AppName)

	if cfg.Systemd {
		service := cfg.AppName + ".service"
		log.Printf("Stopping and disabling systemd service %s", service)
		exec.Command("systemctl", "stop", service).Run()
		exec.Command("systemctl", "disable", service).Run()
		os.Remove("/usr/lib/systemd/system/" + service)
		exec.Command("systemctl", "daemon-reload").Run()
	}

	if cfg.UninstallScript != "" {
		scriptPath := filepath.Join(installDir, filepath.Base(cfg.UninstallScript))
		if !exists(scriptPath) {
			err := copyFileOrDir(cfg.GetUninstallScript(), scriptPath)
			if err != nil {
				log.Fatalf("Failed to copy uninstall script %s: %v", cfg.UninstallScript, err)
			}
		}
		log.Printf("Running uninstall script: %s", scriptPath)
		runScript(scriptPath, installDir)
	} else {
		log.Println("No uninstall script specified, skipping uninstall script step")
	}

	log.Printf("Removing install directory %s", installDir)
	os.RemoveAll(installDir)

	log.Printf("Uninstall completed")
}

func copyFileOrDir(src, dst string) error {
	return cp.Copy(src, dst)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runScript(scriptPath, workDir string) {
	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Script %s failed: %v", scriptPath, err)
	}
}

func installSystemdUnit(cfg *Config) error {
	unit := fmt.Sprintf(`[Unit]
Description=%s service
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=always
User=root

[Install]
WantedBy=multi-user.target
`, cfg.AppName, cfg.Exec)

	unitPath := "/usr/lib/systemd/system/" + cfg.AppName + ".service"
	err := os.WriteFile(unitPath, []byte(unit), 0644)
	if err != nil {
		return err
	}
	log.Printf("Wrote systemd unit to %s", unitPath)

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", cfg.AppName + ".service"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func findTempBuildDir(appName string) (string, error) {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "qp_build_"+appName+"_") {
			return filepath.Join(tmp, e.Name()), nil
		}
	}
	return "", fmt.Errorf("build dir not found")
}
