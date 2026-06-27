package pesalink

import "os"

func ConfigFromEnv() Config {
	return Config{
		APIKey:      os.Getenv("PESALINK_API_KEY"),
		APISecret:   os.Getenv("PESALINK_API_SECRET"),
		PartnerID:   os.Getenv("PESALINK_PARTNER_ID"),
		CallbackURL: os.Getenv("PESALINK_CALLBACK_URL"),
		Sandbox:     os.Getenv("APP_ENV") != "production",
	}
}
