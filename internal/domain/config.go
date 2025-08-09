package domain

// Config represents the application configuration
type Config struct {
	Host          string      `toml:"host" mapstructure:"host"`
	Port          int         `toml:"port" mapstructure:"port"`
	BaseURL       string      `toml:"baseUrl" mapstructure:"baseUrl"`
	SessionSecret string      `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel      string      `toml:"logLevel" mapstructure:"logLevel"`
	LogPath       string      `toml:"logPath" mapstructure:"logPath"`
	Debug         DebugConfig `toml:"debug" mapstructure:"debug"`
	Polar         PolarConfig `toml:"polar" mapstructure:"polar"`
}

// DebugConfig represents debug/profiling configuration
type DebugConfig struct {
	EnableProfiling bool `toml:"enableProfiling" mapstructure:"enableProfiling"`
}

// PolarConfig represents user-configurable Polar settings
// Publisher credentials (AccessToken, OrganizationID, Environment) are baked into the binary
type PolarConfig struct {
	LicenseValidationInterval string `toml:"licenseValidationInterval" mapstructure:"licenseValidationInterval"`
	OfflineGracePeriod        string `toml:"offlineGracePeriod" mapstructure:"offlineGracePeriod"`
}
