package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// Authorizer answers what the caller may do. Declared here, by the code that
// consumes it, and satisfied by usecase/authz.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

// RequirePermission refuses a request whose caller does not hold perm in the
// project the path names.
//
// It exists so that the permission a route declares is enforced by the table
// itself wherever the project can be read from the path. A declaration nothing
// acts on is a comment, and this ticket is about the difference.
//
// Where the project has to be derived from a body or from the row being read,
// the service does the checking instead; there is nothing here to find.
func RequirePermission(
	authz Authorizer,
	perm domain.Permission,
	projectParam string,
	log *slog.Logger,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, ok := session.FromContext(r.Context())
			if !ok {
				// Authenticate runs first and refuses without one, so reaching
				// here means the two were wired in the wrong order.
				refuse(w, http.StatusUnauthorized, "sign in to continue")
				return
			}

			projectID, err := uuid.Parse(r.PathValue(projectParam))
			if err != nil {
				refuse(w, http.StatusBadRequest, "the project id is not a uuid")
				return
			}

			allowed, err := authz.Can(r.Context(), who, perm, projectID)
			if err != nil {
				log.ErrorContext(r.Context(), "could not resolve permissions",
					"path", r.URL.Path, "org_id", who.OrgID, "user_id", who.UserID, "error", err)
				refuse(w, http.StatusInternalServerError, "something went wrong")
				return
			}
			if !allowed {
				log.InfoContext(r.Context(), "request refused",
					"path", r.URL.Path, "org_id", who.OrgID, "user_id", who.UserID,
					"project_id", projectID, "permission", int64(perm))
				refuse(w, http.StatusForbidden, "you do not have permission to do that here")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func refuse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dto.Error{Error: message})
}
