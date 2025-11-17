package main

import (
	"fmt"
	"path/filepath"
)

type FileEntry struct {
	File string `json:"file"`
	From string `json:"from"`
}

type Config struct {
	AppName          string      `json:"app_name"`
	BuildFiles       []string    `json:"build_files"`
	InstallFiles     []FileEntry `json:"install_files"`
	BuildScript      string      `json:"build_script,omitempty"`
	InstallScript    string      `json:"install_script,omitempty"`
	UninstallScript  string      `json:"uninstall_script,omitempty"`
	Systemd          bool        `json:"systemd"`
	SystemdRunAsUser bool        `json:"systemdRunAsUser"`
	Exec             string      `json:"exec,omitempty"`
}

type SystemdUnit struct {
	Name      string
	RunAsUser bool
	ExecPath  string
}

func (s *SystemdUnit) GenerateDescription() string {
	if s.RunAsUser {
		return s.Name + " service running as user %i"
	} else {
		return s.Name + " service"
	}
}

func (s *SystemdUnit) GetUser() any {
	if s.RunAsUser {
		return "%i"
	} else {
		return "root"
	}
}

func (s *SystemdUnit) GenerateFile() string {
	description := s.GenerateDescription()
	user := s.GetUser()
	workingDirectory := filepath.Join(InstallPath, s.Name)

	return fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
ExecStart=%s
WorkingDirectory=%s
Restart=always
User=%s

[Install]
WantedBy=multi-user.target
`, description, s.ExecPath, workingDirectory, user)
}

func (s *SystemdUnit) UnitPath() string {
	return "/usr/lib/systemd/system/" + s.UnitName() + ".service"
}

func (s *SystemdUnit) UnitNameWildcard() string {
	if s.RunAsUser {
		return s.UnitName() + "*"
	} else {
		return s.UnitName()
	}
}

func (s *SystemdUnit) UnitName() string {
	if s.RunAsUser {
		return s.Name + "@"
	} else {
		return s.Name
	}
}

func UnitFromConfig(c *Config) *SystemdUnit {
	return &SystemdUnit{
		Name:      c.AppName,
		ExecPath:  c.Exec,
		RunAsUser: c.SystemdRunAsUser,
	}
}

func (c *Config) GetBuildScript() string {
	return fmt.Sprintf(".qp/%s", c.BuildScript)
}

func (c *Config) GetInstallScript() string {
	return fmt.Sprintf(".qp/%s", c.InstallScript)
}

func (c *Config) GetUninstallScript() string {
	return fmt.Sprintf(".qp/%s", c.UninstallScript)
}
