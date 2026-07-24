import { BriefPage } from '../features/brief/BriefPage'

const views = [
  ['Home', '#/'],
  ['Explore', '#/explore'],
  ['Context Lens', '#/context'],
  ['Impact', '#/impact'],
  ['Knowledge', '#/knowledge'],
  ['Timeline', '#/timeline'],
  ['Setup', '#/setup'],
] as const

type AppProps = {
  sessionError?: boolean
}

export function App({ sessionError = false }: AppProps) {
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
        {sessionError ? (
          <section class="brief-state" aria-label="Secure workbench session">
            <span class="eyebrow">Session security</span>
            <h1>Secure workbench session unavailable</h1>
            <p role="alert">Secure workbench session unavailable. Reopen Prowl from your terminal.</p>
          </section>
        ) : <BriefPage />}
      </main>
    </div>
  )
}
