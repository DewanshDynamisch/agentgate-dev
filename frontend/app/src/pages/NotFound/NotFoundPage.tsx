import { Link } from "react-router-dom";
import { CompassIcon } from "lucide-react";
import { EmptyState } from "../../components/ui/EmptyState";
import { usePageTitle } from "../../lib/usePageTitle";

export function NotFoundPage() {
  usePageTitle("Not found");
  return (
    <div className="ag-page">
      <EmptyState
        icon={CompassIcon}
        title="Page not found"
        subtitle="That page doesn't exist, or the link is out of date."
        action={
          <Link to="/" className="ag-btn ag-btn-primary">
            Back to dashboard
          </Link>
        }
      />
    </div>
  );
}
