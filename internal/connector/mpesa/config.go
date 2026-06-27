package mpesa

import (
	"os"
	"strings"
)

func ConfigFromEnv() Config {
	return Config{
		ConsumerKey:    os.Getenv("MPESA_CONSUMER_KEY"),
		ConsumerSecret: os.Getenv("MPESA_CONSUMER_SECRET"),
		ShortCode:      os.Getenv("MPESA_SHORTCODE"),
		PassKey:        os.Getenv("MPESA_PASSKEY"),
		CallbackBase:   os.Getenv("MPESA_CALLBACK_BASE_URL"),
		Sandbox:        os.Getenv("APP_ENV") != "production",
		B2CInitiatorName: os.Getenv("MPESA_B2C_INITIATOR_NAME"),
		B2CSecurityCred:  os.Getenv("MPESA_B2C_SECURITY_CREDENTIAL"),
		AllowedIPs:       splitCSV(os.Getenv("MPESA_ALLOWED_IPS")),
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
