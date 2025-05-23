package accesslog

import (
	"github.com/yusing/go-proxy/internal/utils"
	"github.com/yusing/go-proxy/pkg/gperr"
)

type (
	Format  string
	Filters struct {
		StatusCodes LogFilter[*StatusCodeRange] `json:"status_codes"`
		Method      LogFilter[HTTPMethod]       `json:"method"`
		Host        LogFilter[Host]             `json:"host"`
		Headers     LogFilter[*HTTPHeader]      `json:"headers"` // header exists or header == value
		CIDR        LogFilter[*CIDR]            `json:"cidr"`
	}
	Fields struct {
		Headers FieldConfig `json:"headers"`
		Query   FieldConfig `json:"query"`
		Cookies FieldConfig `json:"cookies"`
	}
	Config struct {
		BufferSize int        `json:"buffer_size"`
		Format     Format     `json:"format" validate:"oneof=common combined json"`
		Path       string     `json:"path"`
		Stdout     bool       `json:"stdout"`
		Filters    Filters    `json:"filters"`
		Fields     Fields     `json:"fields"`
		Retention  *Retention `json:"retention"`
	}
)

var (
	FormatCommon   Format = "common"
	FormatCombined Format = "combined"
	FormatJSON     Format = "json"

	AvailableFormats = []Format{FormatCommon, FormatCombined, FormatJSON}
)

const DefaultBufferSize = 64 * kilobyte // 64KB

func (cfg *Config) Validate() gperr.Error {
	if cfg.Path == "" && !cfg.Stdout {
		return gperr.New("path or stdout is required")
	}
	return nil
}

func DefaultConfig() *Config {
	return &Config{
		BufferSize: DefaultBufferSize,
		Format:     FormatCombined,
		Retention:  &Retention{Days: 30},
		Fields: Fields{
			Headers: FieldConfig{
				Default: FieldModeDrop,
			},
			Query: FieldConfig{
				Default: FieldModeKeep,
			},
			Cookies: FieldConfig{
				Default: FieldModeDrop,
			},
		},
	}
}

func init() {
	utils.RegisterDefaultValueFactory(DefaultConfig)
}
