const views = [
  ['Home', '#/'],
  ['Explore', '#/explore'],
  ['Context Lens', '#/context'],
  ['Impact', '#/impact'],
  ['Knowledge', '#/knowledge'],
  ['Timeline', '#/timeline'],
  ['Setup', '#/setup'],
] as const

export function App() {
  return (
    <div class="workbench-shell">
      <a class="skip-link" href="#main-content">Skip to content</a>
      <aside class="sidebar" aria-label="Workbench navigation">
        <div class="brand">
          <span class="brand-mark" aria-hidden="true">P</span>
          <div>
            <span class="eyebrow">Local workspace</span>
            <strong>Prowl Workbench</strong>
          </div>
        </div>
        <nav aria-label="Primary">
          <ul>
            {views.map(([label, href], index) => (
              <li key={href}>
                <a href={href} aria-current={index === 0 ? 'page' : undefined}>{label}</a>
              </li>
            ))}
          </ul>
        </nav>
        <p class="privacy-note">Loopback only</p>
      </aside>

      <main id="main-content" tabIndex={-1}>
        <header class="page-header">
          <div>
            <span class="eyebrow">Home / Brief</span>
            <h1>Prowl Workbench</h1>
            <p>A local knowledge compiler for understanding projects, tracing evidence, and preparing precise context for humans and AI agents.</p>
          </div>
          <span class="status-pill">Index ready</span>
        </header>

        <section class="brief-grid" aria-label="Project brief">
          <article class="hero-card">
            <span class="eyebrow">Project purpose</span>
            <h2>Understand what exists before changing it.</h2>
            <p>The guided brief will connect architecture, risky change areas, accepted knowledge, and exact source evidence without opening a full graph.</p>
            <a class="primary-action" href="#/explore">Start guided tour</a>
          </article>
          <article class="metric-card">
            <span class="eyebrow">Architecture</span>
            <strong>Progressive hierarchy</strong>
            <p>Project → domains → flows → evidence</p>
          </article>
          <article class="metric-card">
            <span class="eyebrow">Evidence</span>
            <strong>Deterministic first</strong>
            <p>Every claim keeps its source and freshness.</p>
          </article>
        </section>

        <section class="split-preview" aria-labelledby="preview-heading">
          <div>
            <span class="eyebrow">Next insight</span>
            <h2 id="preview-heading">Your workspace brief will appear here</h2>
            <p>Connect an indexed workspace to identify its purpose, main entrypoints, and the area that deserves the most careful review.</p>
          </div>
          <div class="evidence-placeholder">
            <span class="mono">prowl://workspace/current/source/…</span>
            <span>Exact source evidence</span>
          </div>
        </section>
      </main>
    </div>
  )
}
