// Mirror of dto.Environment in internal/adapter/http/dto. Only what screens
// read so far.
export type VerificationStatus = "pending" | "proof_ok" | "verified" | "verification_failed";

export type Environment = {
  id: string;
  name: string;
  base_url: string;
  verification_status: VerificationStatus;
  max_vus: number;
  max_duration_s: number;
  created_at: string;
};
