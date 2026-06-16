package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	TMDB     TMDBConfig     `mapstructure:"tmdb"`
	Media    MediaConfig    `mapstructure:"media"`
	Transfer TransferConfig `mapstructure:"transfer"`
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
}

type TransferConfig struct {
	Mode             string   `mapstructure:"mode"`
	OverwriteMode    string   `mapstructure:"overwrite_mode"`
	AutoScrape       bool     `mapstructure:"auto_scrape"`
	IgnoreExtensions []string `mapstructure:"ignore_extensions"`
	MinFileSizeMB    int64    `mapstructure:"min_file_size_mb"`
}

var GlobalConfig *Config
var GlobalViper *viper.Viper

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
	v.SetDefault("transfer.min_file_size_mb", 50)

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	GlobalConfig = &config
	return GlobalConfig, nil
}
