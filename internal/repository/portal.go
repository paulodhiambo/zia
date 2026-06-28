package repository

import (
	"context"

	"zia/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, u *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, merchantID, email string) (*domain.User, error)
	UpdateProfile(ctx context.Context, id, name, title, phone string) error
}

type SessionRepository interface {
	Create(ctx context.Context, s *domain.Session) error
	GetByToken(ctx context.Context, token string) (*domain.Session, error)
	DeleteByUserID(ctx context.Context, userID string) error
}

type CustomerRepository interface {
	Create(ctx context.Context, c *domain.Customer) error
	GetByID(ctx context.Context, id string) (*domain.Customer, error)
	ListByMerchant(ctx context.Context, merchantID, search, status string, limit, offset int) ([]domain.Customer, error)
}

type TeamMemberRepository interface {
	Create(ctx context.Context, m *domain.TeamMember) error
	ListByMerchant(ctx context.Context, merchantID string) ([]domain.TeamMember, error)
	GetByEmail(ctx context.Context, merchantID, email string) (*domain.TeamMember, error)
}

type TeamInvitationRepository interface {
	Create(ctx context.Context, inv *domain.TeamInvitation) error
	ListByMerchant(ctx context.Context, merchantID string) ([]domain.TeamInvitation, error)
	GetByEmail(ctx context.Context, merchantID, email string) (*domain.TeamInvitation, error)
	DeleteByEmail(ctx context.Context, merchantID, email string) error
}

type WebhookEndpointRepository interface {
	Create(ctx context.Context, w *domain.WebhookEndpoint) error
	ListByMerchant(ctx context.Context, merchantID string) ([]domain.WebhookEndpoint, error)
}

type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
	ListByMerchant(ctx context.Context, merchantID string) ([]domain.Notification, error)
	MarkAllRead(ctx context.Context, merchantID string) error
}

type userRepo struct{ db DBTX }
type sessionRepo struct{ db DBTX }
type customerRepo struct{ db DBTX }
type teamMemberRepo struct{ db DBTX }
type teamInvitationRepo struct{ db DBTX }
type webhookEndpointRepo struct{ db DBTX }
type notificationRepo struct{ db DBTX }

func NewUserRepo(db DBTX) UserRepository           { return &userRepo{db: db} }
func NewSessionRepo(db DBTX) SessionRepository      { return &sessionRepo{db: db} }
func NewCustomerRepo(db DBTX) CustomerRepository    { return &customerRepo{db: db} }
func NewTeamMemberRepo(db DBTX) TeamMemberRepository { return &teamMemberRepo{db: db} }
func NewTeamInvitationRepo(db DBTX) TeamInvitationRepository { return &teamInvitationRepo{db: db} }
func NewWebhookEndpointRepo(db DBTX) WebhookEndpointRepository { return &webhookEndpointRepo{db: db} }
func NewNotificationRepo(db DBTX) NotificationRepository { return &notificationRepo{db: db} }

func (r *userRepo) Create(ctx context.Context, u *domain.User) error {
	_, err := r.db.Exec(ctx, `INSERT INTO users (id,merchant_id,name,email,password_hash,title,phone,role,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, u.ID, u.MerchantID, u.Name, u.Email, u.PasswordHash, u.Title, u.Phone, u.Role, u.CreatedAt)
	return err
}
func (r *userRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	u := &domain.User{}
	err := r.db.QueryRow(ctx, `SELECT id,merchant_id,name,email,password_hash,title,phone,role,created_at FROM users WHERE id=$1`, id).Scan(&u.ID, &u.MerchantID, &u.Name, &u.Email, &u.PasswordHash, &u.Title, &u.Phone, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}
func (r *userRepo) GetByEmail(ctx context.Context, merchantID, email string) (*domain.User, error) {
	u := &domain.User{}
	err := r.db.QueryRow(ctx, `SELECT id,merchant_id,name,email,password_hash,title,phone,role,created_at FROM users WHERE merchant_id=$1 AND email=$2`, merchantID, email).Scan(&u.ID, &u.MerchantID, &u.Name, &u.Email, &u.PasswordHash, &u.Title, &u.Phone, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}
func (r *userRepo) UpdateProfile(ctx context.Context, id, name, title, phone string) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET name=$1,title=$2,phone=$3 WHERE id=$4`, name, title, phone, id)
	return err
}

func (r *sessionRepo) Create(ctx context.Context, s *domain.Session) error {
	_, err := r.db.Exec(ctx, `INSERT INTO sessions (id,user_id,token,expires_at,created_at) VALUES ($1,$2,$3,$4,$5)`, s.ID, s.UserID, s.Token, s.ExpiresAt, s.CreatedAt)
	return err
}
func (r *sessionRepo) GetByToken(ctx context.Context, token string) (*domain.Session, error) {
	s := &domain.Session{}
	err := r.db.QueryRow(ctx, `SELECT id,user_id,token,expires_at,created_at FROM sessions WHERE token=$1 AND expires_at>now()`, token).Scan(&s.ID, &s.UserID, &s.Token, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}
func (r *sessionRepo) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
	return err
}

func (r *customerRepo) Create(ctx context.Context, c *domain.Customer) error {
	_, err := r.db.Exec(ctx, `INSERT INTO customers (id,merchant_id,name,company,email,phone,location,volume_minor,ltv_minor,status,payment_method,joined_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, c.ID, c.MerchantID, c.Name, c.Company, c.Email, c.Phone, c.Location, c.VolumeMinor, c.LTVMinor, c.Status, c.PaymentMethod, c.JoinedAt, c.CreatedAt)
	return err
}
func (r *customerRepo) GetByID(ctx context.Context, id string) (*domain.Customer, error) {
	c := &domain.Customer{}
	err := r.db.QueryRow(ctx, `SELECT id,merchant_id,name,company,email,phone,location,volume_minor,ltv_minor,status,payment_method,joined_at,created_at FROM customers WHERE id=$1`, id).Scan(&c.ID, &c.MerchantID, &c.Name, &c.Company, &c.Email, &c.Phone, &c.Location, &c.VolumeMinor, &c.LTVMinor, &c.Status, &c.PaymentMethod, &c.JoinedAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}
func (r *customerRepo) ListByMerchant(ctx context.Context, merchantID, search, status string, limit, offset int) ([]domain.Customer, error) {
	query := `SELECT id,merchant_id,name,company,email,phone,location,volume_minor,ltv_minor,status,payment_method,joined_at,created_at FROM customers WHERE merchant_id=$1`
	args := []any{merchantID}
	argIdx := 2
	if search != "" {
		query += ` AND (name ILIKE $` + itoa(argIdx) + ` OR company ILIKE $` + itoa(argIdx) + ` OR email ILIKE $` + itoa(argIdx) + `)`
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if status != "" && status != "All" {
		query += ` AND status=$` + itoa(argIdx)
		args = append(args, status)
		argIdx++
	}
	query += ` ORDER BY created_at DESC LIMIT $` + itoa(argIdx) + ` OFFSET $` + itoa(argIdx+1)
	args = append(args, limit, offset)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cs []domain.Customer
	for rows.Next() {
		var c domain.Customer
		if err := rows.Scan(&c.ID, &c.MerchantID, &c.Name, &c.Company, &c.Email, &c.Phone, &c.Location, &c.VolumeMinor, &c.LTVMinor, &c.Status, &c.PaymentMethod, &c.JoinedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, nil
}

func (r *teamMemberRepo) Create(ctx context.Context, m *domain.TeamMember) error {
	_, err := r.db.Exec(ctx, `INSERT INTO team_members (id,merchant_id,user_id,name,email,role,initials,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, m.ID, m.MerchantID, m.UserID, m.Name, m.Email, m.Role, m.Initials, m.CreatedAt)
	return err
}
func (r *teamMemberRepo) ListByMerchant(ctx context.Context, merchantID string) ([]domain.TeamMember, error) {
	rows, err := r.db.Query(ctx, `SELECT id,merchant_id,user_id,name,email,role,last_active,initials,created_at FROM team_members WHERE merchant_id=$1 ORDER BY created_at ASC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ms []domain.TeamMember
	for rows.Next() {
		var m domain.TeamMember
		if err := rows.Scan(&m.ID, &m.MerchantID, &m.UserID, &m.Name, &m.Email, &m.Role, &m.LastActive, &m.Initials, &m.CreatedAt); err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return ms, nil
}
func (r *teamMemberRepo) GetByEmail(ctx context.Context, merchantID, email string) (*domain.TeamMember, error) {
	m := &domain.TeamMember{}
	err := r.db.QueryRow(ctx, `SELECT id,merchant_id,user_id,name,email,role,last_active,initials,created_at FROM team_members WHERE merchant_id=$1 AND email=$2`, merchantID, email).Scan(&m.ID, &m.MerchantID, &m.UserID, &m.Name, &m.Email, &m.Role, &m.LastActive, &m.Initials, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *teamInvitationRepo) Create(ctx context.Context, inv *domain.TeamInvitation) error {
	_, err := r.db.Exec(ctx, `INSERT INTO team_invitations (id,merchant_id,email,role,token,expires_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, inv.ID, inv.MerchantID, inv.Email, inv.Role, inv.Token, inv.ExpiresAt, inv.CreatedAt)
	return err
}
func (r *teamInvitationRepo) ListByMerchant(ctx context.Context, merchantID string) ([]domain.TeamInvitation, error) {
	rows, err := r.db.Query(ctx, `SELECT id,merchant_id,email,role,token,expires_at,created_at FROM team_invitations WHERE merchant_id=$1 ORDER BY created_at DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invs []domain.TeamInvitation
	for rows.Next() {
		var inv domain.TeamInvitation
		if err := rows.Scan(&inv.ID, &inv.MerchantID, &inv.Email, &inv.Role, &inv.Token, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		invs = append(invs, inv)
	}
	return invs, nil
}
func (r *teamInvitationRepo) GetByEmail(ctx context.Context, merchantID, email string) (*domain.TeamInvitation, error) {
	inv := &domain.TeamInvitation{}
	err := r.db.QueryRow(ctx, `SELECT id,merchant_id,email,role,token,expires_at,created_at FROM team_invitations WHERE merchant_id=$1 AND email=$2`, merchantID, email).Scan(&inv.ID, &inv.MerchantID, &inv.Email, &inv.Role, &inv.Token, &inv.ExpiresAt, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}
func (r *teamInvitationRepo) DeleteByEmail(ctx context.Context, merchantID, email string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM team_invitations WHERE merchant_id=$1 AND email=$2`, merchantID, email)
	return err
}

func (r *webhookEndpointRepo) Create(ctx context.Context, w *domain.WebhookEndpoint) error {
	_, err := r.db.Exec(ctx, `INSERT INTO webhook_endpoints (id,merchant_id,url,events,status,created_at) VALUES ($1,$2,$3,$4,$5,$6)`, w.ID, w.MerchantID, w.URL, w.Events, w.Status, w.CreatedAt)
	return err
}
func (r *webhookEndpointRepo) ListByMerchant(ctx context.Context, merchantID string) ([]domain.WebhookEndpoint, error) {
	rows, err := r.db.Query(ctx, `SELECT id,merchant_id,url,events,status,created_at FROM webhook_endpoints WHERE merchant_id=$1 ORDER BY created_at DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ws []domain.WebhookEndpoint
	for rows.Next() {
		var w domain.WebhookEndpoint
		if err := rows.Scan(&w.ID, &w.MerchantID, &w.URL, &w.Events, &w.Status, &w.CreatedAt); err != nil {
			return nil, err
		}
		ws = append(ws, w)
	}
	return ws, nil
}

func (r *notificationRepo) Create(ctx context.Context, n *domain.Notification) error {
	_, err := r.db.Exec(ctx, `INSERT INTO notifications (id,merchant_id,tone,title,body,category,unread,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, n.ID, n.MerchantID, n.Tone, n.Title, n.Body, n.Category, n.Unread, n.CreatedAt)
	return err
}
func (r *notificationRepo) ListByMerchant(ctx context.Context, merchantID string) ([]domain.Notification, error) {
	rows, err := r.db.Query(ctx, `SELECT id,merchant_id,tone,title,body,category,unread,created_at FROM notifications WHERE merchant_id=$1 ORDER BY created_at DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ns []domain.Notification
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.MerchantID, &n.Tone, &n.Title, &n.Body, &n.Category, &n.Unread, &n.CreatedAt); err != nil {
			return nil, err
		}
		ns = append(ns, n)
	}
	return ns, nil
}
func (r *notificationRepo) MarkAllRead(ctx context.Context, merchantID string) error {
	_, err := r.db.Exec(ctx, `UPDATE notifications SET unread=false WHERE merchant_id=$1`, merchantID)
	return err
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
