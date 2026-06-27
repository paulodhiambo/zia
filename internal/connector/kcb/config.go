package kcb

import "os"

func ConfigFromEnv() Config {
	return Config{
		ConsumerKey:    os.Getenv("KCB_CONSUMER_KEY"),
		ConsumerSecret: os.Getenv("KCB_CONSUMER_SECRET"),
		OrgShortCode:   os.Getenv("KCB_ORG_SHORTCODE"),
		CallbackURL:    os.Getenv("KCB_CALLBACK_URL"),
		Sandbox:        os.Getenv("APP_ENV") != "production",
	}
}
