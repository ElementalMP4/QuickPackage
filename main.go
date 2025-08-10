package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"log"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

type Config struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	InstallPath    string   `json:"install_path"`
	Files          []string `json:"files"`
	Dependencies   []string `json:"dependencies"`
	RunCommand     string   `json:"run_command"`
	ServiceUser    string   `json:"service_user"`
	Maintainer     string   `json:"maintainer"`
	PrebuildScript string   `json:"prebuild_script,omitempty"`
	PostinstScript string   `json:"postinst_script,omitempty"`
}

func main() {
	configPath := ".qp/config.json"
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read config file %s: %v", configPath, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	buildDir, err := os.MkdirTemp("", "quickpackage-"+cfg.Name)
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(buildDir)
	fmt.Println("Building in", buildDir)

	debianDir := filepath.Join(buildDir, "debian")
	if err := os.Mkdir(debianDir, 0755); err != nil {
		log.Fatalf("Failed to create debian dir: %v", err)
	}

	writeControl(debianDir, &cfg)
	writeRules(debianDir)
	writePreinst(debianDir, &cfg)
	writeInstall(debianDir, &cfg)
	writePostinst(debianDir, &cfg)
	writePrerm(debianDir)
	writeService(debianDir, &cfg)
	writeChangelog(debianDir, &cfg)

	appDir := filepath.Join(buildDir, cfg.InstallPath[1:])
	if err := os.MkdirAll(appDir, 0755); err != nil {
		log.Fatalf("Failed to create app dir: %v", err)
	}

	copyFiles(appDir, cfg.Files)

	if err := os.Chdir(buildDir); err != nil {
		log.Fatalf("Failed to chdir: %v", err)
	}

	cmd := exec.Command("dpkg-buildpackage", "-us", "-uc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Println("Running dpkg-buildpackage...")
	if err := cmd.Run(); err != nil {
		log.Fatalf("dpkg-buildpackage failed: %v", err)
	}

	fmt.Println("Package built successfully!")
}

func writeChangelog(dir string, cfg *Config) {
	content := fmt.Sprintf(`%s (%s) stable; urgency=low

  * QuickPackage update

  -- %s  %s

`, cfg.Name, cfg.Version, cfg.Maintainer, time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700"))

	err := os.WriteFile(filepath.Join(dir, "changelog"), []byte(content), 0644)
	if err != nil {
		log.Fatalf("Failed to write changelog: %v", err)
	}
}

func writeControl(dir string, cfg *Config) {
	controlTemplate := `Source: {{.Name}}
Maintainer: {{.Maintainer}}
Section: utils
Priority: optional
Standards-Version: 4.5.0

Package: {{.Name}}
Architecture: all
Depends: {{ join .Dependencies ", " }}
Description: {{.Name}} application packaged by QuickPackage
`

	tmpl, err := template.New("control").Funcs(template.FuncMap{
		"join": func(arr []string, sep string) string {
			return joinStrings(arr, sep)
		},
	}).Parse(controlTemplate)
	if err != nil {
		log.Fatalf("Template parse error: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, cfg)
	if err != nil {
		log.Fatalf("Template exec error: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir, "control"), buf.Bytes(), 0644)
	if err != nil {
		log.Fatalf("Failed to write control: %v", err)
	}
}

func joinStrings(arr []string, sep string) string {
	result := ""
	for i, s := range arr {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func writeRules(dir string) {
	rules := `#!/usr/bin/make -f
%:
	dh $@
`
	path := filepath.Join(dir, "rules")
	err := os.WriteFile(path, []byte(rules), 0755)
	if err != nil {
		log.Fatalf("Failed to write rules: %v", err)
	}
}

func writeInstall(dir string, cfg *Config) {
	var lines []string
	for _, pattern := range cfg.Files {
		lines = append(lines, fmt.Sprintf("%s %s/", pattern, cfg.InstallPath))
	}
	lines = append(lines, fmt.Sprintf("debian/%s.service etc/systemd/system/", cfg.Name))

	err := os.WriteFile(filepath.Join(dir, "install"), []byte(fmt.Sprintln(lines)), 0644)
	if err != nil {
		log.Fatalf("Failed to write install: %v", err)
	}
}

func writePreinst(dir string, cfg *Config) {
	path := filepath.Join(dir, "preinst")

	if cfg.PrebuildScript != "" {
		data, err := os.ReadFile(cfg.PrebuildScript)
		if err != nil {
			log.Fatalf("Failed to read prebuild script %s: %v", cfg.PrebuildScript, err)
		}
		err = os.WriteFile(path, data, 0755)
		if err != nil {
			log.Fatalf("Failed to write preinst: %v", err)
		}
		return
	}

	defaultPreinst := `#!/bin/bash
set -e
# Default preinst script
exit 0
`
	err := os.WriteFile(path, []byte(defaultPreinst), 0755)
	if err != nil {
		log.Fatalf("Failed to write default preinst: %v", err)
	}
}

func writePostinst(dir string, cfg *Config) {
	path := filepath.Join(dir, "postinst")

	var userScriptPart string
	if cfg.PostinstScript != "" {
		userScriptName := "custom_postinst.sh"
		userScriptDest := filepath.Join(dir, userScriptName)

		data, err := os.ReadFile(cfg.PostinstScript)
		if err != nil {
			log.Fatalf("Failed to read user postinst script %s: %v", cfg.PostinstScript, err)
		}

		err = os.WriteFile(userScriptDest, data, 0755)
		if err != nil {
			log.Fatalf("Failed to write user postinst script to debian dir: %v", err)
		}

		userScriptPart = fmt.Sprintf("\n# Run user-defined postinst script\n./%s \"$@\"\n", userScriptName)
	}

	postinst := fmt.Sprintf(`#!/bin/bash
set -e

%s

if ! id %s >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin %s
fi

mkdir -p %s
chown -R %s:%s %s

systemctl daemon-reload
systemctl enable %s.service
systemctl restart %s.service

exit 0
`,
		userScriptPart,
		cfg.ServiceUser,
		cfg.ServiceUser,
		cfg.InstallPath,
		cfg.ServiceUser,
		cfg.ServiceUser,
		cfg.InstallPath,
		cfg.Name,
		cfg.Name,
	)

	err := os.WriteFile(path, []byte(postinst), 0755)
	if err != nil {
		log.Fatalf("Failed to write postinst: %v", err)
	}
}

func writePrerm(dir string) {
	prerm := `#!/bin/bash
set -e
systemctl stop receiptify.service || true
systemctl disable receiptify.service || true
systemctl daemon-reload
exit 0
`
	err := os.WriteFile(filepath.Join(dir, "prerm"), []byte(prerm), 0755)
	if err != nil {
		log.Fatalf("Failed to write prerm: %v", err)
	}
}

func writeService(dir string, cfg *Config) {
	service := fmt.Sprintf(`[Unit]
Description=%s service
After=network.target

[Service]
Type=simple
User=%s
Group=%s
WorkingDirectory=%s
ExecStart=%s
Restart=on-failure

[Install]
WantedBy=multi-user.target
`, cfg.Name, cfg.ServiceUser, cfg.ServiceUser, cfg.InstallPath, cfg.RunCommand)

	err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.service", cfg.Name)), []byte(service), 0644)
	if err != nil {
		log.Fatalf("Failed to write service: %v", err)
	}
}

func copyFiles(dest string, patterns []string) {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.Fatalf("Invalid glob %s: %v", pattern, err)
		}
		for _, src := range matches {
			data, err := os.ReadFile(src)
			if err != nil {
				log.Fatalf("Failed to read %s: %v", src, err)
			}
			dstPath := filepath.Join(dest, filepath.Base(src))
			err = os.WriteFile(dstPath, data, 0644)
			if err != nil {
				log.Fatalf("Failed to write %s: %v", dstPath, err)
			}
			fmt.Println("Copied", src, "to", dstPath)
		}
	}
}
