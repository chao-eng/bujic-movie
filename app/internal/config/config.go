package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Network  NetworkConfig  `mapstructure:"network"`
	TMDB     TMDBConfig     `mapstructure:"tmdb"`
	Media    MediaConfig    `mapstructure:"media"`
	Transfer TransferConfig `mapstructure:"transfer"`
	Search   SearchConfig   `mapstructure:"search"`
}

// NetworkConfig is the shared outbound proxy. Proxies are configured once here
// and each consumer opts in, because scraping (TMDB, images) and indexer
// searches need different egress in practice: a search proxy is often required
// for tracker sites while TMDB may be reachable directly, or the other way
// round.
type NetworkConfig struct {
	// Proxy is the default egress address. Supports http/https/socks5/socks5h.
	Proxy string `mapstructure:"proxy"`
	// ScrapeViaProxy routes TMDB API and image downloads through Proxy.
	ScrapeViaProxy bool `mapstructure:"scrape_via_proxy"`
	// SearchViaProxy routes indexer and definition-source requests through
	// Proxy. A site-level proxy still overrides this.
	SearchViaProxy bool `mapstructure:"search_via_proxy"`
}

// SearchConfig controls the indexer/search/download module. Every field is
// optional; defaults keep the module usable without a config file.
type SearchConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	DefinitionSource    string `mapstructure:"definition_source"`
	DefinitionVersion   string `mapstructure:"definition_version"`
	AutoSync            bool   `mapstructure:"auto_sync"`
	Proxy               string `mapstructure:"proxy"`
	MaxDefinitions      int    `mapstructure:"max_definitions"`
	MaxGlobalSearches   int    `mapstructure:"max_global_searches"`
	MaxSiteWorkers      int    `mapstructure:"max_site_workers"`
	SearchDeadlineMS    int    `mapstructure:"search_deadline_ms"`
	SiteTimeoutMS       int    `mapstructure:"site_timeout_ms"`
	DefaultIntervalMS   int    `mapstructure:"default_interval_ms"`
	GrabTimeoutMS       int    `mapstructure:"grab_timeout_ms"`
	GrabWindowMS        int    `mapstructure:"grab_window_ms"`
	RecordRetentionDays int    `mapstructure:"record_retention_days"`
	HistoryLimit        int    `mapstructure:"history_limit"`
	// AliasResolution rewrites Chinese titles into their TMDB original titles
	// before querying indexers.
	AliasResolution bool `mapstructure:"alias_resolution"`
	// AliasTimeoutMS bounds alias resolution so it never eats the search budget.
	AliasTimeoutMS int `mapstructure:"alias_timeout_ms"`
}

type ServerConfig struct {
	Port      int    `mapstructure:"port"`
	Mode      string `mapstructure:"mode"`
	SecretKey string `mapstructure:"secret_key"`
	Username  string `mapstructure:"username"`
	Password  string `mapstructure:"password"`
}

type DatabaseConfig struct {
	DBPath string `mapstructure:"db_path"`
}

type TMDBConfig struct {
	APIKey   string `mapstructure:"api_key"`
	BaseURL  string `mapstructure:"base_url"`
	Language string `mapstructure:"language"`
}

type MediaConfig struct {
	MoviePath    string `mapstructure:"movie_path"`
	TVPath       string `mapstructure:"tv_path"`
	DownloadPath string `mapstructure:"download_path"`
	// LockNFO controls <lockdata> in the generated NFO. When true, media
	// servers (Jellyfin/Emby/Kodi) will not overwrite the NFO on scan.
	// Default false mirrors the Jellyfin reference NFO behaviour.
	LockNFO bool `mapstructure:"lock_nfo"`
}

type TransferConfig struct {
	Mode             string   `mapstructure:"mode"`
	OverwriteMode    string   `mapstructure:"overwrite_mode"`
	AutoScrape       bool     `mapstructure:"auto_scrape"`
	ScrapePerson     bool     `mapstructure:"scrape_person"`
	IgnoreExtensions []string `mapstructure:"ignore_extensions"`
	MinFileSizeMB    int64    `mapstructure:"min_file_size_mb"`
}

var GlobalConfig *Config
var GlobalViper *viper.Viper

// EffectiveSearchProxy returns the proxy indexer traffic should use. A site
// level override is applied later, per request.
func (c *Config) EffectiveSearchProxy() string {
	if c == nil || !c.Network.SearchViaProxy {
		return ""
	}
	return c.networkProxy()
}

// EffectiveScrapeProxy returns the proxy TMDB traffic should use.
func (c *Config) EffectiveScrapeProxy() string {
	if c == nil || !c.Network.ScrapeViaProxy {
		return ""
	}
	return c.networkProxy()
}

// networkProxy prefers the shared setting and falls back to the legacy
// search-only address so an existing installation keeps working after upgrade.
func (c *Config) networkProxy() string {
	if strings.TrimSpace(c.Network.Proxy) != "" {
		return c.Network.Proxy
	}
	return c.Search.Proxy
}

// LoadConfig loads configuration from environment variables and defaults (no config.yaml)
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()
	GlobalViper = v

	// Environment variable overrides (e.g., BUJIC_SERVER_PORT)
	v.SetEnvPrefix("BUJIC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.secret_key", "change_me_to_something_secure")
	v.SetDefault("server.username", "admin")
	v.SetDefault("server.password", "admin123")
	v.SetDefault("database.db_path", "data/bujic-movie.db")
	v.SetDefault("tmdb.base_url", "https://api.themoviedb.org/3")
	v.SetDefault("tmdb.language", "zh-CN")
	v.SetDefault("transfer.mode", "link")
	v.SetDefault("transfer.overwrite_mode", "size")
	v.SetDefault("transfer.auto_scrape", true)
	v.SetDefault("transfer.scrape_person", false)
	v.SetDefault("transfer.min_file_size_mb", 50)
	v.SetDefault("media.lock_nfo", false)
	v.SetDefault("network.proxy", "")
	v.SetDefault("network.scrape_via_proxy", false)
	v.SetDefault("network.search_via_proxy", true)
	v.SetDefault("search.enabled", true)
	v.SetDefault("search.definition_source", "https://indexers.prowlarr.com")
	v.SetDefault("search.definition_version", "12")
	v.SetDefault("search.auto_sync", true)
	v.SetDefault("search.max_definitions", 1000)
	v.SetDefault("search.max_global_searches", 3)
	v.SetDefault("search.max_site_workers", 40)
	v.SetDefault("search.search_deadline_ms", 15000)
	v.SetDefault("search.site_timeout_ms", 8000)
	v.SetDefault("search.default_interval_ms", 2000)
	v.SetDefault("search.grab_timeout_ms", 15000)
	v.SetDefault("search.grab_window_ms", 60000)
	v.SetDefault("search.record_retention_days", 90)
	v.SetDefault("search.history_limit", 20)
	v.SetDefault("search.alias_resolution", true)
	v.SetDefault("search.alias_timeout_ms", 4000)

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	GlobalConfig = &config
	return GlobalConfig, nil
}
