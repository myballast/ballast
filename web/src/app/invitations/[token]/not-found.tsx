export default function NotFound() {
  return (
    <main className="flex flex-1 items-center justify-center px-4 py-16">
      <div className="flex max-w-md flex-col gap-1.5">
        <h1 className="text-3xl font-normal tracking-tight">No such invitation</h1>
        <p className="text-sm text-muted-foreground">
          This link does not match any invitation. Check that it was copied whole, or ask an admin of the project for a
          new one.
        </p>
      </div>
    </main>
  );
}
