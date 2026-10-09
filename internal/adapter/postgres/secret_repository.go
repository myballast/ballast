package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// Compile-time proof that this repository satisfies both ports: the one the
// environment service declared, which cannot read a value back, and the one the
// run service declared, which is the only thing in Ballast that can.
var (
	_ environment.Secrets = (*SecretRepository)(nil)
	_ run.SecretReader    = (*SecretRepository)(nil)
)

// Cipher seals and opens a stored value. Declared here, by the code that uses
// it, and implemented in infra/crypto.
//
// The encryption lives in this layer rather than above it because it is a
// property of how the row is stored: nothing in a use case has to know that the
// column holds a ciphertext, and nothing above this line holds the key.
type Cipher interface {
	// Seal encrypts plaintext. aad is authenticated but not stored, and must be
	// supplied again to open the result.
	Seal(aad []byte, plaintext string) ([]byte, error)
	Open(aad, sealed []byte) (string, error)
}

// SecretRepository stores the credentials an environment's scenarios need, and
// hands them to the generator at launch.
//
// It is one type serving two ports deliberately: the encryption and the
// decryption have to agree about the associated data, and splitting them into two
// files is how they stop agreeing.
type SecretRepository struct {
	db     *DB
	cipher Cipher
}

// NewSecretRepository returns a repository over db, encrypting with cipher.
func NewSecretRepository(db *DB, cipher Cipher) *SecretRepository {
	return &SecretRepository{db: db, cipher: cipher}
}

// secretAAD is what a ciphertext is bound to: the organization, the environment
// and the key.
//
// Supplying it again is what opening requires, so a ciphertext copied into
// another row — another organization's environment, or another key of the same
// one — no longer opens. Without it, somebody who can write to this table but
// holds no key could move a value they control into a row they can read back, or
// move a row they cannot read into an environment of their own and recover it by
// launching a run.
func secretAAD(orgID, environmentID uuid.UUID, key string) []byte {
	return []byte(orgID.String() + "/" + environmentID.String() + "/" + key)
}

// Keys lists what is set, without the values. The query does not select the
// ciphertext column at all.
func (r *SecretRepository) Keys(
	ctx context.Context, orgID, environmentID uuid.UUID,
) ([]domain.Secret, error) {
	var found []domain.Secret

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListSecretKeys(ctx,
			sqlc.ListSecretKeysParams{OrgID: orgID, EnvironmentID: environmentID})
		if err != nil {
			return fmt.Errorf("list the secret keys: %w", err)
		}

		found = make([]domain.Secret, 0, len(rows))
		for _, row := range rows {
			found = append(found, domain.Secret{
				ID:            row.ID,
				EnvironmentID: row.EnvironmentID,
				Key:           row.Key,
				CreatedAt:     row.CreatedAt,
				UpdatedAt:     row.UpdatedAt,
			})
		}
		return nil
	})

	return found, err
}

// Put stores a value under key, replacing whatever was there.
//
// The value is encrypted before the statement is built, so the plaintext never
// reaches the connection — not as a bound parameter, and so not into any
// statement log the database keeps.
func (r *SecretRepository) Put(
	ctx context.Context, orgID, environmentID uuid.UUID, key, value string, at time.Time,
) (domain.Secret, error) {
	sealed, err := r.cipher.Seal(secretAAD(orgID, environmentID, key), value)
	if err != nil {
		// Deliberately not wrapped with anything about the value.
		return domain.Secret{}, fmt.Errorf("encrypt the secret: %w", err)
	}

	var stored domain.Secret
	err = r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).UpsertSecret(ctx, sqlc.UpsertSecretParams{
			ID:             uuid.New(),
			OrgID:          orgID,
			EnvironmentID:  environmentID,
			Key:            key,
			ValueEncrypted: sealed,
			CreatedAt:      at,
		})
		if err != nil {
			return writeError(err, "write the secret")
		}

		stored = domain.Secret{
			ID:            row.ID,
			EnvironmentID: row.EnvironmentID,
			Key:           row.Key,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		return nil
	})

	return stored, err
}

// Remove drops a credential.
func (r *SecretRepository) Remove(
	ctx context.Context, orgID, environmentID uuid.UUID, key string,
) error {
	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).DeleteSecret(ctx, sqlc.DeleteSecretParams{
			OrgID:         orgID,
			EnvironmentID: environmentID,
			Key:           key,
		})
		if err != nil {
			return fmt.Errorf("delete the secret: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// SecretsOf returns an environment's secrets, decrypted, for injection into the
// generator.
//
// A value that will not open stops the run rather than being skipped. A scenario
// missing the credential it authenticates with does not measure anything, and a
// row that fails to open means either the key is wrong or somebody has been
// editing the table — neither is a thing to carry on through.
func (r *SecretRepository) SecretsOf(
	ctx context.Context, orgID, environmentID uuid.UUID,
) (map[string]string, error) {
	values := map[string]string{}

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).ListSecrets(ctx,
			sqlc.ListSecretsParams{OrgID: orgID, EnvironmentID: environmentID})
		if err != nil {
			return fmt.Errorf("read the secrets: %w", err)
		}

		for _, row := range rows {
			value, err := r.cipher.Open(secretAAD(orgID, row.EnvironmentID, row.Key), row.ValueEncrypted)
			if err != nil {
				// The key is named, the value is not.
				return fmt.Errorf("the secret %s could not be decrypted: %w", row.Key, err)
			}
			values[row.Key] = value
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}
