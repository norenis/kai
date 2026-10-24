package cli

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"runtime"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
	"github.com/norenis/kai/internal/dashboard"
	"github.com/norenis/kai/internal/events"
	bsync "github.com/norenis/kai/internal/sync"
	"github.com/spf13/cobra"
)

func newDashboardCmd() *cobra.Command {
	var port int
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the brain dashboard web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			brainPath := config.ExpandPath(cfg.Brain.Path)
			if brainPath == "" {
				brainPath = config.ExpandPath(config.DefaultBrainPath)
			}

			// Pick a random available port if not specified.
			if !cmd.Flags().Changed("port") {
				p, err := randomPort()
				if err != nil {
					return fmt.Errorf("find available port: %w", err)
				}
				port = p
			}

			b := brain.New(brainPath)
			bus := events.NewBus()
			b.SetEvents(bus, events.SourceDashboard)
			srv := dashboard.New(b, brainPath, cfg)
			srv.SetEvents(bus)

			// Initialize syncer if configured.
			syncer := dashboardSyncer(cfg, brainPath)
			if syncer != nil {
				srv.SetSyncer(syncer)
				slog.Info("sync backends available for dashboard")
			}

			// Determine bind host from config (default 0.0.0.0) with --port override.
			bindHost := "0.0.0.0"
			if cfgHost, _, err := net.SplitHostPort(cfg.Server.ListenAddr); err == nil && cfgHost != "" {
				bindHost = cfgHost
			}
			addr := fmt.Sprintf("%s:%d", bindHost, port)
			localURL := fmt.Sprintf("http://localhost:%d", port)

			fmt.Printf("%s dashboard: %s\n", cfg.AgentName(), localURL)
			if bindHost == "0.0.0.0" || bindHost == "" {
				if lanIP := lanIPAddress(); lanIP != "" {
					fmt.Printf("%s dashboard (LAN): http://%s:%d\n", cfg.AgentName(), lanIP, port)
				}
			}

			if !noOpen {
				openBrowser(localURL)
			}

			return srv.ListenAndServe(addr)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "Port to serve on (default: random available)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't auto-open the browser")

	return cmd
}

func randomPort() (int, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Start()
}

// lanIPAddress returns the first non-loopback IPv4 address, or empty string.
func lanIPAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

func dashboardSyncer(cfg *config.Config, brainPath string) *bsync.Syncer {
	var gitBackend bsync.Backend
	if cfg.Brain.Sync.Git.Enabled {
		gitBackend = bsync.NewGitBackend(brainPath, cfg.Brain.Sync.Git.Remote, cfg.Brain.Sync.Git.Branch)
	}

	if gitBackend == nil {
		return nil
	}
	return bsync.NewSyncer(brainPath, gitBackend, nil)
}
