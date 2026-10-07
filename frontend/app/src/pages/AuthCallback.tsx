import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../state/AuthProvider";

export function AuthCallbackPage() {
  const { completeLogin } = useAuth();
  const navigate = useNavigate();
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    completeLogin().then(
      () => navigate("/", { replace: true }),
      () => setFailed(true)
    );
  }, [completeLogin, navigate]);

  if (failed) {
    return (
      <div style={{ padding: 32 }}>
        <div className="ag-alert ag-alert-error">Sign-in failed. Please try again.</div>
        <Link to="/login">Back to sign in</Link>
      </div>
    );
  }
  return <div style={{ padding: 32 }}>Completing sign-in...</div>;
}
