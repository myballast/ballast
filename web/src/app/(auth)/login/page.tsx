import { getProviders, safeNext } from "@/features/auth/api";
import { SignIn } from "@/features/auth/components/sign-in";

export default async function LoginPage(props: PageProps<"/login">) {
  const { next, error } = await props.searchParams;
  const providers = await getProviders();

  return (
    <main className="flex flex-1 items-center justify-center px-4 py-16">
      <SignIn
        providers={providers}
        next={safeNext(next)}
        error={typeof error === "string" ? error : undefined}
      />
    </main>
  );
}
