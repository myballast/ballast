export default function NotFound() {
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-3xl font-light tracking-tight">Run not found</h1>
      <p className="text-sm text-muted-foreground">
        There is no run at this address, or it belongs to another organization.
      </p>
    </div>
  );
}
