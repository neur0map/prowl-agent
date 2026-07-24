import { render } from 'preact'

import { App } from './app/App'
import './styles.css'
import { bootstrapWorkbenchSession } from './transport/auth'

const root = document.getElementById('app')
if (!root) throw new Error('workbench root not found')

void bootstrapWorkbenchSession()
  .then(() => render(<App />, root))
  .catch(() => {
    root.textContent = 'Unable to establish a secure workbench session.'
  })
