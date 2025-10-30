package version

var (
	Version    = "0.5"
	GitHash    = "devsbXXX"
	BuildTS    = "2022-03-09T16:14:05.999999999Z07:00" // to be replaced at build time
	APIVersion = "1.0"
	OS         = "FluxOS"
	Model      = "EV1500"
	DeviceCode = Model
	Agent      = OS + "/" + Version
	Branch     = "eval"
	User       = "user"
)

type VersionConfig struct {
	Version          string `json:"Version"`
	GitHash          string `json:"GitHash"`
	BuildTS          string `json:"BuildTS"`
	OS               string `json:"OS"`
	Model            string `json:"Model"`
	DeviceCode       string `json:"DeviceCode"`
	Agent            string `json:"Agent"`
	Branch           string `json:"Branch"`
	User             string `json:"user"`
	SkipTLS          bool   `json:"SkipTLS"`
	SkipTLSFwupgrade bool   `json:"SkipTLSFwupgrade"`
	LocalAPI         string `json:"LocalAPI"`
}

var versionconfig = VersionConfig{
	Version:    Version,
	GitHash:    GitHash,
	BuildTS:    BuildTS,
	OS:         OS,
	Model:      Model,
	DeviceCode: DeviceCode,
	Agent:      Agent,
	Branch:     Branch,
	User:       User,
}

func GetVersionConfig() VersionConfig {
	return versionconfig
}
