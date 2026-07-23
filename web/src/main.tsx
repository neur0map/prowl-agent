import { render } from 'preact'

import { App } from './app/App'
import './styles.css'
import { consumeLaunchToken } from './transport/auth'

consumeLaunchToken()
const root = document.getElementById('app')
if (!root) throw new Error('workbench root not found')
render(<App />, root)
