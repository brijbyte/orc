package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brijbyte/orc/internal/config"
	orcservice "github.com/brijbyte/orc/internal/service"
	"github.com/spf13/cobra"
)

func newServiceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the background web service",
	}
	cmd.AddCommand(newServiceInstallCommand())
	cmd.AddCommand(serviceAction("start", "Start the service", orcservice.Start, true))
	cmd.AddCommand(serviceAction("stop", "Stop the service", orcservice.Stop, false))
	cmd.AddCommand(serviceAction("restart", "Restart the service", orcservice.Restart, true))
	cmd.AddCommand(serviceAction("uninstall", "Stop and remove the service", orcservice.Uninstall, false))
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the service status and URL",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			printServiceStatus(false)
		},
	})
	return cmd
}

func newServiceInstallCommand() *cobra.Command {
	var addr, domain, cwd string
	var port int
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and start the service for this user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			addr, err = resolveWebAddr(addr, port, cmd.Flags().Changed("port"))
			if err != nil {
				return err
			}
			password, created, err := config.EnsureWebAuth()
			if err != nil {
				return err
			}
			if created {
				fmt.Println("web password: " + password)
			}
			bin, err := executablePath()
			if err != nil {
				return err
			}
			cwd, err = filepath.Abs(cwd)
			if err != nil {
				return err
			}
			if err := orcservice.Install(orcservice.Options{
				Executable: bin,
				Addr:       addr,
				Domain:     domain,
				Cwd:        cwd,
			}); err != nil {
				return err
			}
			fmt.Println("orc service installed and started")
			printServiceStatus(true)
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "serve", "127.0.0.1", "web UI address")
	cmd.Flags().IntVar(&port, "port", 7777, "web UI port (env PORT; default 7777)")
	cmd.Flags().StringVar(&domain, "domain", "", "public domain with TLS on port 443")
	cmd.Flags().StringVar(&cwd, "cwd", ".", "default working directory")
	return cmd
}

func serviceAction(use, short string, action func() error, wait bool) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := action(); err != nil {
				return err
			}
			fmt.Printf("orc service %s complete\n", use)
			printServiceStatus(wait)
			return nil
		},
	}
}

func printServiceStatus(wait bool) {
	if wait {
		for range 30 {
			if data, err := os.ReadFile(orcservice.URLPath()); err == nil && len(data) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	status := orcservice.GetStatus()
	switch {
	case status.Running:
		fmt.Println("status: running")
		if data, err := os.ReadFile(orcservice.URLPath()); err == nil {
			fmt.Printf("url: %s\n", strings.TrimSpace(string(data)))
		} else {
			fmt.Println("url: not ready")
		}
		fmt.Printf("log: %s\n", orcservice.LogPath())
	case status.Installed:
		fmt.Println("status: stopped")
	case strings.HasPrefix(status.Detail, "unsupported"):
		fmt.Printf("status: %s\n", status.Detail)
	default:
		fmt.Println("status: not installed")
	}
}

func executablePath() (string, error) {
	path := os.Args[0]
	if found, err := exec.LookPath(path); err == nil {
		path = found
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("orc executable: %w", err)
	}
	return path, nil
}
