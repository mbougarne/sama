export function OverviewPage() {
  return (
    <section aria-labelledby="overview-title">
      <p className="eyebrow">Your cloud, clearly</p>
      <h1 id="overview-title">Cloud overview</h1>
      <p className="intro">One backoffice for the cloud services you manage.</p>
      <div className="empty-state">
        <h2>Your workspace starts here</h2>
        <p>
          Account access and cloud connections will be available in a future
          release.
        </p>
        <p className="muted">
          Sama manages your services. Payments stay with your providers.
        </p>
      </div>
    </section>
  );
}
