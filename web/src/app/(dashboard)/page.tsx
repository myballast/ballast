// Where a sign-in lands when it was not started from a particular page. The
// project screens that will live here come with their own ticket.
export default function HomePage() {
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-3xl font-light tracking-tight">Ballast</h1>
      <p className="text-sm text-muted-foreground">
        Open a run from its link to see its results.
      </p>
    </div>
  );
}
