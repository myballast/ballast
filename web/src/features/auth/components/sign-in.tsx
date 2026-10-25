import { buttonVariants } from "@/components/ui/button";

import { loginURL } from "../login-url";

const labels: Record<string, string> = {
  github: "Continue with GitHub",
  google: "Continue with Google",
};

// The codes the API puts in /login?error=. Never its own messages.
const errors: Record<string, string> = {
  denied: "Sign-in was cancelled at the provider.",
  invalid_request: "That sign-in link has expired or was not started here. Try again.",
  signin_failed: "Sign-in did not go through. Try again.",
};

export function SignIn({
  providers,
  next,
  error,
}: {
  providers: string[];
  next: string;
  error?: string;
}) {
  return (
    <div className="flex w-full max-w-sm flex-col gap-6">
      <div className="flex flex-col gap-1.5">
        <h1 className="text-3xl font-normal tracking-tight">Sign in to Ballast</h1>
        <p className="text-sm text-muted-foreground">
          Ballast keeps no password. Sign in with an account you already have.
        </p>
      </div>

      {error && (
        <p role="alert" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          {errors[error] ?? errors.signin_failed}
        </p>
      )}

      {providers.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No identity provider is configured on the API.
        </p>
      ) : (
        <div className="flex flex-col gap-2">
          {providers.map((provider) => (
            <a
              key={provider}
              href={loginURL(provider, next)}
              className={buttonVariants({ size: "lg" })}
            >
              {labels[provider] ?? `Continue with ${provider}`}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
