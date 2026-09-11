import { MapPinOff } from "lucide-react";
import { Link } from "react-router";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";

export function NotFoundPage() {
  return (
    <EmptyState
      icon={MapPinOff}
      title="No page here"
      actions={
        <Button asChild variant="primary">
          <Link to="/">Go to picks</Link>
        </Button>
      }
    >
      <p>This address doesn't match anything in Proposarr.</p>
    </EmptyState>
  );
}
