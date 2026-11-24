package entity

import "time"

// SearchIndexerDefinition is a Cardigann YAML definition synced from the
// upstream definition source or imported locally.
type SearchIndexerDefinition struct {
	ID               string    `gorm:"primaryKey;column:id" json:"id"`
	Name             string    `gorm:"column:name;not null" json:"name"`
	Source           string    `gorm:"column:source;not null" json:"source"` // remote / local
	Version          string    `gorm:"column:version;not null" json:"version"`
	Type             string    `gorm:"column:type;not null" json:"type"`
	Language         string    `gorm:"column:language" json:"language"`
	CapabilitiesJSON string    `gorm:"column:capabilities_json;not null" json:"capabilities_json"`
	Compat           string    `gorm:"column:compat;not null" json:"compat"` // full / partial / invalid
	CompatDetail     string    `gorm:"column:compat_detail" json:"compat_detail"`
	RawYAML          string    `gorm:"column:raw_yaml;not null" json:"-"`
	RawHash          string    `gorm:"column:raw_hash;not null;index" json:"raw_hash"`
	UpdatedAt        time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (SearchIndexerDefinition) TableName() string {
	return "search_indexer_definitions"
}

// SearchIndexer is a configured indexer instance: either a Cardigann
// definition instance or a generic Torznab/Newznab endpoint.
type SearchIndexer struct {
	ID                uint      `gorm:"primaryKey;column:id" json:"id"`
	CreatedAt         time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
	Name              string    `gorm:"column:name;not null;uniqueIndex" json:"name"`
	Connector         string    `gorm:"column:connector;not null" json:"connector"` // cardigann / torznab
	DefinitionID      string    `gorm:"column:definition_id;index" json:"definition_id"`
	DefinitionVersion string    `gorm:"column:definition_version" json:"definition_version"`
	DefinitionYAML    string    `gorm:"column:definition_yaml" json:"-"`
	BaseURL           string    `gorm:"column:base_url" json:"base_url"`
	APIKeyEnc         string    `gorm:"column:api_key_enc" json:"-"`
	SettingsJSON      string    `gorm:"column:settings_json" json:"settings"`
	SecretsEnc        string    `gorm:"column:secrets_enc" json:"-"`
	Categories        string    `gorm:"column:categories" json:"categories"`
	ProxyURLEnc       string    `gorm:"column:proxy_url_enc" json:"-"`
	ExtraCookiesEnc   string    `gorm:"column:extra_cookies_enc" json:"-"`
	UserAgent         string    `gorm:"column:user_agent" json:"user_agent"`
	MinIntervalMS     int       `gorm:"column:min_interval_ms;not null;default:2000" json:"min_interval_ms"`
	Priority          int       `gorm:"column:priority;not null;default:50" json:"priority"`
	DownloadClientID  uint      `gorm:"column:download_client_id;not null;default:0" json:"download_client_id"`
	Enabled           bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	// SearchLanguages overrides how many languages this site is searched in,
	// comma separated ("zh,en"). Empty means "follow the definition". A site like
	// FileMood indexes both Chinese and English releases, so searching it once
	// per language finds more than picking a single one.
	SearchLanguages string `gorm:"column:search_languages;not null;default:''" json:"-"`
}

func (SearchIndexer) TableName() string {
	return "search_indexers"
}

// SearchIndexerSession keeps the login cookie for a Cardigann indexer.
type SearchIndexerSession struct {
	IndexerID uint      `gorm:"primaryKey;column:indexer_id" json:"indexer_id"`
	State     string    `gorm:"column:state;not null" json:"state"` // none / pending / logged-in / failed
	CookieEnc string    `gorm:"column:cookie_enc" json:"-"`
	ExpiresAt time.Time `gorm:"column:expires_at" json:"expires_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SearchIndexerSession) TableName() string {
	return "search_indexer_sessions"
}

// SearchIndexerStatus tracks health, cooldown and latency for one indexer.
type SearchIndexerStatus struct {
	IndexerID           uint      `gorm:"primaryKey;column:indexer_id" json:"indexer_id"`
	LastSuccessAt       time.Time `gorm:"column:last_success_at" json:"last_success_at"`
	ConsecutiveFailures int       `gorm:"column:consecutive_failures;not null;default:0" json:"consecutive_failures"`
	CooldownUntil       time.Time `gorm:"column:cooldown_until" json:"cooldown_until"`
	AvgElapsedMS        int       `gorm:"column:avg_elapsed_ms;not null;default:0" json:"avg_elapsed_ms"`
	SampleCount         int       `gorm:"column:sample_count;not null;default:0" json:"sample_count"`
	LastErrorKind       string    `gorm:"column:last_error_kind" json:"last_error_kind"`
}

func (SearchIndexerStatus) TableName() string {
	return "search_indexer_status"
}

// SearchDownloadClient is a download client instance. Kind selects the
// implementation in service/downloadclient, whose Capabilities decide which of
// the fields below actually apply.
type SearchDownloadClient struct {
	ID              uint      `gorm:"primaryKey;column:id" json:"id"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at" json:"updated_at"`
	Name            string    `gorm:"column:name;not null;uniqueIndex" json:"name"`
	Kind            string    `gorm:"column:kind;not null" json:"kind"`
	Host            string    `gorm:"column:host;not null" json:"host"`
	Port            int       `gorm:"column:port;not null" json:"port"`
	UseSSL          bool      `gorm:"column:use_ssl;not null;default:false" json:"use_ssl"`
	URLBase         string    `gorm:"column:url_base" json:"url_base"`
	APIKeyEnc       string    `gorm:"column:api_key_enc" json:"-"`
	Username        string    `gorm:"column:username" json:"username"`
	PasswordEnc     string    `gorm:"column:password_enc" json:"-"`
	DefaultCategory string    `gorm:"column:default_category" json:"default_category"`
	// DefaultSavePath is the fallback download directory for clients that cannot
	// derive one from a category (Transmission, Deluge, Aria2). Empty means the
	// client's own default directory.
	DefaultSavePath     string    `gorm:"column:default_save_path" json:"default_save_path"`
	InitialState        string    `gorm:"column:initial_state;not null;default:start" json:"initial_state"`
	Sequential          bool      `gorm:"column:sequential;not null;default:false" json:"sequential"`
	FirstLastFirst      bool      `gorm:"column:first_last_first;not null;default:false" json:"first_last_first"`
	ContentLayout       string    `gorm:"column:content_layout;not null;default:original" json:"content_layout"`
	Priority            int       `gorm:"column:priority;not null;default:50" json:"priority"`
	IsDefault           bool      `gorm:"column:is_default;not null;default:false" json:"is_default"`
	Enabled             bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	ConsecutiveFailures int       `gorm:"column:consecutive_failures;not null;default:0" json:"consecutive_failures"`
	UnavailableUntil    time.Time `gorm:"column:unavailable_until" json:"unavailable_until"`
	LastSuccessAt       time.Time `gorm:"column:last_success_at" json:"last_success_at"`
}

func (SearchDownloadClient) TableName() string {
	return "search_download_clients"
}

// SearchDownloadClientCategory maps a Newznab category (or group) to a
// download client category. SavePath is optional and only used when the client
// cannot turn a category into a folder by itself (Transmission, Deluge, Aria2):
// there the mapping is what tells the module where the file should land.
type SearchDownloadClientCategory struct {
	ID             uint   `gorm:"primaryKey;column:id" json:"id"`
	DownloaderID   uint   `gorm:"column:downloader_id;not null;index:idx_search_client_source,unique" json:"downloader_id"`
	SourceCategory int    `gorm:"column:source_category;not null;index:idx_search_client_source,unique" json:"source_category"`
	ClientCategory string `gorm:"column:client_category;not null" json:"client_category"`
	SavePath       string `gorm:"column:save_path" json:"save_path"`
}

func (SearchDownloadClientCategory) TableName() string {
	return "search_download_client_categories"
}

// SearchDownloadRecord records one grab attempt. Indexer and downloader names
// are snapshotted so records stay readable after deletion.
type SearchDownloadRecord struct {
	ID             uint   `gorm:"primaryKey;column:id" json:"id"`
	Title          string `gorm:"column:title;not null" json:"title"`
	IndexerID      uint   `gorm:"column:indexer_id;not null;index" json:"indexer_id"`
	IndexerName    string `gorm:"column:indexer_name;not null" json:"indexer_name"`
	DownloaderID   uint   `gorm:"column:downloader_id;not null;index" json:"downloader_id"`
	DownloaderName string `gorm:"column:downloader_name;not null" json:"downloader_name"`
	SizeBytes      int64  `gorm:"column:size_bytes;not null;default:0" json:"size_bytes"`
	ClientCategory string `gorm:"column:client_category" json:"client_category"`
	// SavePath is what was actually passed to the downloader. Empty means the
	// client used its own default.
	SavePath    string    `gorm:"column:save_path" json:"save_path"`
	Status      string    `gorm:"column:status;not null;index" json:"status"` // created / failed / pending
	ErrorKind   string    `gorm:"column:error_kind" json:"error_kind"`
	InfoHash    string    `gorm:"column:info_hash;index" json:"info_hash"`
	Fingerprint string    `gorm:"column:fingerprint;not null;index:idx_search_grab_idem" json:"fingerprint"`
	CreatedAt   time.Time `gorm:"column:created_at;not null;index" json:"created_at"`
}

func (SearchDownloadRecord) TableName() string {
	return "search_download_records"
}

// SearchHistory keeps the most recent search keywords.
type SearchHistory struct {
	ID         uint      `gorm:"primaryKey;column:id" json:"id"`
	Keyword    string    `gorm:"column:keyword;not null;uniqueIndex" json:"keyword"`
	UseCount   int       `gorm:"column:use_count;not null;default:1" json:"use_count"`
	LastUsedAt time.Time `gorm:"column:last_used_at;not null;index" json:"last_used_at"`
}

func (SearchHistory) TableName() string {
	return "search_history"
}
