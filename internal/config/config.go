package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	LPVersion  = "1.47"
	LEVersion  = "1.80"
	BASVersion = "2.2"

	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/145.0.0.0 Safari/537.36"
)

type Config struct {
	School       string `json:"school,omitempty"`
	LMSHost      string `json:"lms_host,omitempty"`
	SyllabusHost string `json:"syllabus_host,omitempty"`
	DownloadDir  string `json:"download_dir,omitempty"`
}

type Paths struct {
	Dir            string
	Config         string
	Token          string
	BrowserProfile string
	Downloads      string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("home directory: %w", err)
	}

	dir := filepath.Join(home, ".d2l")

	return Paths{
		Dir:            dir,
		Config:         filepath.Join(dir, "config.json"),
		Token:          filepath.Join(dir, "token.json"),
		BrowserProfile: filepath.Join(dir, "browser_profile"),
		Downloads:      filepath.Join(dir, "downloads"),
	}, nil
}

func Load(paths Paths) (Config, error) {
	var cfg Config

	data, err := os.ReadFile(paths.Config)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if value := os.Getenv("D2L_HOST"); value != "" {
		cfg.LMSHost = value
	}
	if value := os.Getenv("D2L_SYLLABUS_HOST"); value != "" {
		cfg.SyllabusHost = value
	}
	if value := os.Getenv("D2L_DOWNLOAD_DIR"); value != "" {
		cfg.DownloadDir = value
	}

	return cfg, nil
}

func Save(paths Paths, cfg Config) error {
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	data = append(data, '\n')

	return atomicWrite(paths.Config, data, 0o600)
}

func Validate(cfg *Config, allowPrivate bool) error {
	var err error
	cfg.LMSHost, err = NormalizeHost(cfg.LMSHost, allowPrivate)
	if err != nil {
		return fmt.Errorf("invalid Brightspace host: %w", err)
	}

	if cfg.SyllabusHost != "" {
		cfg.SyllabusHost, err = NormalizeHost(cfg.SyllabusHost, allowPrivate)
		if err != nil {
			return fmt.Errorf("invalid SimpleSyllabus host: %w", err)
		}
	}

	return nil
}

func NormalizeHost(raw string, allowPrivate bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("host is required")
	}

	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("HTTPS is required")
	}
	if u.User != nil || u.Hostname() == "" {
		return "", errors.New("credentials and empty hostnames are not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("query strings and fragments are not allowed")
	}

	host := strings.ToLower(u.Hostname())
	if !allowPrivate {
		if host == "localhost" {
			return "", errors.New("localhost is not allowed")
		}

		ip := net.ParseIP(host)
		if ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
			return "", errors.New("private and link-local addresses are not allowed")
		}
	}

	port := u.Port()
	if port != "" {
		host = net.JoinHostPort(host, port)
	}

	return "https://" + host, nil
}

func DownloadRoot(paths Paths, cfg Config) string {
	if cfg.DownloadDir != "" {
		return cfg.DownloadDir
	}
	return paths.Downloads
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}

	name := tmp.Name()
	defer os.Remove(name)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(name, path)
}
