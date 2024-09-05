package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type AuthZService struct {
	key string
}

func NewAuthZService(key string) AuthZService {
	return AuthZService{key: key}
}

func (s AuthZService) Authorize(ctx context.Context, permName string, objKind string, objId string) error {
	tokenString, ok := ctx.Value("authz-token").(string)
	if !ok {
		return errors.New("no token provided")
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.key), nil
	})
	if err != nil {
		return err
	}

	var permissions []string
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if permissionsClaim, ok := claims["permissions"].(string); ok {
			permissions = strings.Split(permissionsClaim, ",")
		} else {
			return errors.New("custom claim permissions is not a string or does not exist")
		}
	} else {
		return errors.New("invalid claims type")
	}

	reqPerm := fmt.Sprintf("%s|%s|%s", permName, objKind, objId)
	for _, perm := range permissions {
		if perm == reqPerm {
			return nil
		}
	}

	return errors.New("required permission not found")
}
