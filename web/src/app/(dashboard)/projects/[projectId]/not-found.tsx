export default function NotFound() {
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-3xl font-normal tracking-tight">Project not found</h1>
      <p className="text-sm text-muted-foreground">
        There is no project at this address, or you are not one of its members.
      </p>
    </div>
  );
}
