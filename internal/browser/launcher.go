package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const ExtensionID = "mmlmfjhmonkocbjadbfplnigmagldckm"

type LaunchOptions struct {
	BrowserPath    string
	ExtensionToken string
	RelayEndpoint  string
	ClientName     string
}

func Launch(options LaunchOptions) error {
	path := options.BrowserPath
	if path == "" {
		var err error
		path, err = FindEdge()
		if err != nil {
			return err
		}
	}
	connectURL, err := ConnectURL(options)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, connectURL)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Edge: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release Edge process: %w", err)
	}
	return nil
}

func ConnectURL(options LaunchOptions) (string, error) {
	if options.RelayEndpoint == "" {
		return "", errors.New("extension relay endpoint is required")
	}
	clientName := options.ClientName
	if clientName == "" {
		clientName = "亚马逊选品专家"
	}
	client, err := json.Marshal(map[string]any{"name": clientName})
	if err != nil {
		return "", err
	}
	u := &url.URL{
		Scheme: "chrome-extension",
		Host:   ExtensionID,
		Path:   "/connect.html",
	}
	query := u.Query()
	query.Set("mcpRelayUrl", options.RelayEndpoint)
	query.Set("client", string(client))
	query.Set("protocolVersion", "2")
	if options.ExtensionToken != "" {
		query.Set("token", options.ExtensionToken)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func FindEdge() (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			filepath.Join(os.Getenv("HOME"), "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
		}
	case "windows":
		for _, root := range []string{os.Getenv("PROGRAMFILES(X86)"), os.Getenv("PROGRAMFILES"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				candidates = append(candidates, filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"))
			}
		}
	case "linux":
		for _, name := range []string{"microsoft-edge", "microsoft-edge-stable", "msedge"} {
			if path, err := exec.LookPath(name); err == nil {
				return path, nil
			}
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("Microsoft Edge executable not found; set BrowserPath")
}
