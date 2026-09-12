package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// PermissionWords says what the person may do, in words.
func PermissionWords(permissions *models.EffectivePermissions) string {
	if permissions == nil {
		return "nothing beyond their own mail"
	}
	var words []string
	for _, permission := range models.Permissions() {
		if !permissions.HasAnywhere(permission) {
			continue
		}
		domains, all := permissions.DomainsWith(permission)
		switch {
		case all || len(domains) == 0:
			words = append(words, string(permission))
		default:
			words = append(words, fmt.Sprintf("%s (over %d domain(s))", permission, len(domains)))
		}
	}
	if len(words) == 0 {
		return "nothing beyond their own mail"
	}
	sort.Strings(words)
	return strings.Join(words, ", ")
}
