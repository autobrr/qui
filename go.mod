module github.com/autobrr/qui

go 1.25

require (
	github.com/CAFxX/httpcompression v0.0.9
	github.com/Hellseher/go-shellquote v0.1.0
	github.com/Masterminds/semver/v3 v3.4.0
	github.com/alexedwards/scs/v2 v2.9.0
	github.com/anacrolix/torrent v1.59.1
	github.com/andybalholm/brotli v1.2.0
	github.com/autobrr/autobrr v1.70.0
	github.com/autobrr/go-qbittorrent v1.15.0-rc1.0.20251209201933-62cc902b8602
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/coreos/go-oidc/v3 v3.17.0
	github.com/creativeprojects/go-selfupdate v1.5.1
	github.com/expr-lang/expr v1.17.7
	github.com/fsnotify/fsnotify v1.9.0
	github.com/go-chi/chi/v5 v5.2.3
	github.com/hashicorp/go-version v1.8.0
	github.com/keygen-sh/machineid v1.1.1
	github.com/klauspost/compress v1.18.2
	github.com/lithammer/fuzzysearch v1.1.8
	github.com/mat/besticon/v3 v3.21.0
	github.com/moistari/rls v0.6.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.23.2
	github.com/rs/cors v1.11.1
	github.com/rs/zerolog v1.34.0
	github.com/spf13/cobra v1.10.1
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.11.1
	github.com/ulikunitz/xz v0.5.15
	golang.org/x/crypto v0.45.0
	golang.org/x/image v0.33.0
	golang.org/x/net v0.47.0
	golang.org/x/oauth2 v0.33.0
	golang.org/x/sync v0.18.0
	golang.org/x/term v0.37.0
	golang.org/x/text v0.31.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.40.1
)

replace github.com/moistari/rls => github.com/autobrr/rls v0.7.1-0.20251216090501-a93fa08d24d2

require (
	code.gitea.io/sdk/gitea v0.22.0 // indirect
	github.com/42wim/httpsig v1.2.3 // indirect
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/anacrolix/generics v0.1.0 // indirect
	github.com/anacrolix/missinggo v1.3.0 // indirect
	github.com/anacrolix/missinggo/v2 v2.10.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/davidmz/go-pageant v1.0.2 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-fed/httpsig v1.1.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/go-github/v30 v30.1.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-varint v0.0.6 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/xanzy/go-gitlab v0.115.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20250819193227-8b4c13bb791b // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	lukechampine.com/blake3 v1.1.6 // indirect
	modernc.org/libc v1.66.10 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
