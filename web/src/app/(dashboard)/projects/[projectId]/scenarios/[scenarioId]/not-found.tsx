export default function NotFound() {
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-3xl font-normal tracking-tight">Scenario not found</h1>
      <p className="text-sm text-muted-foreground">
        There is no scenario at this address in this project, or you are not one of the project&apos;s members.
      </p>
    </div>
  );
}
