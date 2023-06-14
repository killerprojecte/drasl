package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"github.com/BurntSushi/toml"
	"log"
	"lukechampine.com/blake3"
	"os"
	"path"
)

type rateLimitConfig struct {
	Enable            bool
	RequestsPerSecond float64
}

type bodySizeLimitConfig struct {
	Enable    bool
	SizeLimit string
}

type FallbackAPIServer struct {
	Nickname   string
	SessionURL string
	AccountURL string
	SkinDomain string
}

type anonymousLoginConfig struct {
	Allow         bool
	UsernameRegex string
	Password      string
}

type registrationNewPlayerConfig struct {
	Allow             bool
	AllowChoosingUUID bool
}

type registrationExistingPlayerConfig struct {
	Allow                   bool
	Nickname                string
	SessionURL              string
	AccountURL              string
	SetSkinURL              string
	RequireSkinVerification bool
}

type Config struct {
	InstanceName               string
	StateDirectory             string
	DataDirectory              string
	ApplicationOwner           string
	Domain                     string
	BaseURL                    string
	ListenAddress              string
	RateLimit                  rateLimitConfig
	BodySize                   bodySizeLimitConfig
	SignPublicKeys             bool
	DisableTokenExpiry         bool
	LogRequests                bool
	HideListenAddress          bool
	DefaultPreferredLanguage   string
	SkinSizeLimit              int
	AllowChangingPlayerName    bool
	MinPasswordLength          int
	SkinForwarding             bool
	FallbackAPIServers         []FallbackAPIServer
	AnonymousLogin             anonymousLoginConfig
	RegistrationNewPlayer      registrationNewPlayerConfig
	RegistrationExistingPlayer registrationExistingPlayerConfig
}

var defaultRateLimitConfig = rateLimitConfig{
	Enable:            true,
	RequestsPerSecond: 10,
}

func DefaultConfig() Config {
	return Config{
		InstanceName:             "Drasl",
		Domain:                   "drasl.example.com",
		StateDirectory:           "/var/lib/drasl",
		DataDirectory:            "/usr/share/drasl",
		ApplicationOwner:         "Unmojang",
		BaseURL:                  "https://drasl.example.com",
		ListenAddress:            "0.0.0.0:9090",
		RateLimit:                defaultRateLimitConfig,
		LogRequests:              true,
		SignPublicKeys:           false,
		DefaultPreferredLanguage: "en",
		SkinSizeLimit:            128,
		AllowChangingPlayerName:  true,
		HideListenAddress:        false,
		SkinForwarding:           true,
		MinPasswordLength:        1,
		DisableTokenExpiry:       false,
		FallbackAPIServers: []FallbackAPIServer{{
			Nickname:   "Mojang",
			SessionURL: "https://sessionserver.mojang.com",
			AccountURL: "https://api.mojang.com",
		}},
		AnonymousLogin: anonymousLoginConfig{
			Allow: false,
		},
		RegistrationNewPlayer: registrationNewPlayerConfig{
			Allow:             true,
			AllowChoosingUUID: false,
		},
		RegistrationExistingPlayer: registrationExistingPlayerConfig{
			Allow:                   true,
			Nickname:                "Mojang",
			SessionURL:              "https://sessionserver.mojang.com",
			AccountURL:              "https://api.mojang.com",
			SetSkinURL:              "https://www.minecraft.net/msaprofile/mygames/editskin",
			RequireSkinVerification: true,
		},
	}
}

func ReadOrCreateConfig(path string) *Config {
	config := DefaultConfig()

	_, err := os.Stat(path)
	if err != nil {
		// File doesn't exist? Try to create it
		f, err := os.Create(path)
		Check(err)

		defer f.Close()

		err = toml.NewEncoder(f).Encode(config)
		Check(err)
	}

	_, err = toml.DecodeFile(path, &config)

	// Config post-processing
	log.Println("Loaded config: ", config)
	Check(err)

	return &config
}

func KeyB3Sum512(key *rsa.PrivateKey) []byte {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	Check(err)

	sum := blake3.Sum512(der)
	return sum[:]
}

func ReadOrCreateKey(config *Config) *rsa.PrivateKey {
	err := os.MkdirAll(config.StateDirectory, os.ModePerm)
	Check(err)
	path := path.Join(config.StateDirectory, "key.pkcs8")

	der, err := os.ReadFile(path)
	if err == nil {
		key, err := x509.ParsePKCS8PrivateKey(der)
		Check(err)

		return key.(*rsa.PrivateKey)
	} else {
		key, err := rsa.GenerateKey(rand.Reader, 4096)
		Check(err)

		der, err := x509.MarshalPKCS8PrivateKey(key)
		Check(err)
		err = os.WriteFile(path, der, 0600)
		Check(err)

		return key
	}
}
