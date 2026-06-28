package domain

import (
	"encoding/json"
	"time"
)

type User struct {
	ID           string    `db:"id" json:"id"`
	MerchantID   string    `db:"merchant_id" json:"-"`
	Name         string    `db:"name" json:"name"`
	Email        string    `db:"email" json:"email"`
	PasswordHash string    `db:"password_hash" json:"-"`
	Title        string    `db:"title" json:"title"`
	Phone        string    `db:"phone" json:"phone"`
	Role         string    `db:"role" json:"role"`
	CreatedAt    time.Time `db:"created_at" json:"-"`
}

type Session struct {
	ID         string    `db:"id" json:"-"`
	UserID     string    `db:"user_id" json:"-"`
	MerchantID string    `db:"merchant_id" json:"-"`
	Token      string    `db:"token" json:"token"`
	ExpiresAt  time.Time `db:"expires_at" json:"expiresAt"`
	CreatedAt  time.Time `db:"created_at" json:"-"`
}

type Customer struct {
	ID            string    `db:"id" json:"id"`
	MerchantID    string    `db:"merchant_id" json:"-"`
	Name          string    `db:"name" json:"name"`
	Company       string    `db:"company" json:"company"`
	Email         string    `db:"email" json:"email"`
	Phone         string    `db:"phone" json:"phone"`
	Location      string    `db:"location" json:"location"`
	VolumeMinor   int64     `db:"volume_minor" json:"-"`
	LTVMinor      int64     `db:"ltv_minor" json:"-"`
	Status        string    `db:"status" json:"status"`
	PaymentMethod string    `db:"payment_method" json:"paymentMethod"`
	JoinedAt      time.Time `db:"joined_at" json:"joined"`
	CreatedAt     time.Time `db:"created_at" json:"-"`
}

type TeamMember struct {
	ID         string     `db:"id" json:"-"`
	MerchantID string     `db:"merchant_id" json:"-"`
	UserID     string     `db:"user_id" json:"-"`
	Name       string     `db:"name" json:"name"`
	Email      string     `db:"email" json:"email"`
	Role       string     `db:"role" json:"role"`
	LastActive *time.Time `db:"last_active" json:"-"`
	Initials   string     `db:"initials" json:"initials"`
	CreatedAt  time.Time  `db:"created_at" json:"-"`
}

type TeamInvitation struct {
	ID         string    `db:"id" json:"-"`
	MerchantID string    `db:"merchant_id" json:"-"`
	Email      string    `db:"email" json:"email"`
	Role       string    `db:"role" json:"role"`
	Token      string    `db:"token" json:"-"`
	ExpiresAt  time.Time `db:"expires_at" json:"-"`
	CreatedAt  time.Time `db:"created_at" json:"invited"`
}

type WebhookEndpoint struct {
	ID         string    `db:"id" json:"-"`
	MerchantID string    `db:"merchant_id" json:"-"`
	URL        string    `db:"url" json:"url"`
	Events     int       `db:"events" json:"events"`
	Status     string    `db:"status" json:"status"`
	CreatedAt  time.Time `db:"created_at" json:"-"`
}

type NotificationPreferences struct {
	MerchantID  string          `db:"merchant_id" json:"-"`
	Preferences json.RawMessage `db:"preferences" json:"preferences"`
	UpdatedAt   time.Time       `db:"updated_at" json:"updatedAt"`
}

type Notification struct {
	ID         string    `db:"id" json:"id"`
	MerchantID string    `db:"merchant_id" json:"-"`
	Tone       string    `db:"tone" json:"tone"`
	Title      string    `db:"title" json:"title"`
	Body       string    `db:"body" json:"body"`
	Category   string    `db:"category" json:"category"`
	Unread     bool      `db:"unread" json:"unread"`
	CreatedAt  time.Time `db:"created_at" json:"time"`
}
