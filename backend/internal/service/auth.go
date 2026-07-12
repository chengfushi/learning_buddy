package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// AuthService 账号体系（F1）：注册/登录/JWT/角色。
type AuthService struct {
	repos *repository.Repositories
	cfg   *config.Config
}

func NewAuthService(repos *repository.Repositories, cfg *config.Config) *AuthService {
	return &AuthService{repos: repos, cfg: cfg}
}

// Claims JWT 载荷。
type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Register 注册新用户：默认 student，自动创建私有 team（F2.3）。
// role 仅允许 student/teacher（super_admin 只能通过种子写入，防止越权提权）。
func (s *AuthService) Register(ctx context.Context, email, password, displayName, role string) (*model.User, error) {
	if email == "" || password == "" {
		return nil, errors.New("邮箱与密码必填")
	}
	if role == "" {
		role = "student"
	}
	if role != "student" && role != "teacher" {
		return nil, errors.New("不允许注册该角色")
	}
	if len(password) < 6 {
		return nil, errors.New("密码至少 6 位")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	h := string(hash)
	u := &model.User{
		Email:        email,
		PasswordHash: &h,
		DisplayName:  displayName,
		Role:         role,
		Subscription: "free",
	}
	// 注册 + 自动建私有 team（同一事务，保证原子性）
	err = s.repos.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		privateTeam := &model.Team{
			Name:    fmt.Sprintf("%s 的私有资料", displayName),
			Type:    "private",
			OwnerID: &u.ID,
		}
		if err := tx.Create(privateTeam).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Login 校验密码并返回 access / refresh 令牌。
func (s *AuthService) Login(ctx context.Context, email, password string) (*model.User, string, string, error) {
	var u model.User
	if err := s.repos.DB.WithContext(ctx).First(&u, "email = ?", email).Error; err != nil {
		return nil, "", "", errors.New("账号或密码错误")
	}
	if u.PasswordHash == nil {
		return nil, "", "", errors.New("账号或密码错误")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(password)); err != nil {
		return nil, "", "", errors.New("账号或密码错误")
	}
	access, err := s.sign(u.ID, u.Role, time.Hour*24)
	if err != nil {
		return nil, "", "", err
	}
	refresh, err := s.sign(u.ID, u.Role, time.Hour*24*7)
	if err != nil {
		return nil, "", "", err
	}
	return &u, access, refresh, nil
}

// Refresh 用 refresh token 换发 access token。
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (string, error) {
	claims, err := s.parse(refreshToken)
	if err != nil {
		return "", errors.New("无效 refresh token")
	}
	return s.sign(claims.UserID, claims.Role, time.Hour*24)
}

// VerifyToken 校验 access token，返回 claims。
func (s *AuthService) VerifyToken(tokenStr string) (*Claims, error) {
	return s.parse(tokenStr)
}

func (s *AuthService) sign(userID int64, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(s.cfg.JWTSecret))
}

func (s *AuthService) parse(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
