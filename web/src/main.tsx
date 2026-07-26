import { render } from 'preact'

import { App } from './app/App'
import { I18nProvider } from './i18n'
import './styles.css'
import { bootstrapWorkbenchSession } from './transport/auth'

const root = document.getElementById('app')
if (!root) throw new Error('workbench root not found')

void bootstrapWorkbenchSession()
  .then(() => render(<I18nProvider><App /></I18nProvider>, root))
  .catch(() => render(<I18nProvider><App sessionError /></I18nProvider>, root))
