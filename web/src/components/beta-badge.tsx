import { Badge } from "@/components/ui/badge";

// BetaBadge says, next to the name, that the service is a beta and what that
// means for whoever pays: nothing yet. Neutral on purpose: it is not a status,
// and violet is for actions.
export function BetaBadge({ className }: { className?: string }) {
  return (
    <Badge variant="outline" title="Free during the beta" className={`text-muted-foreground ${className ?? ""}`}>
      Beta
      <span className="sr-only">, free during the beta</span>
    </Badge>
  );
}
