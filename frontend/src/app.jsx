import { useState, useEffect } from 'react'
import Calculator from './components/calculator'
import History from './components/history'
import Auth from './components/auth'
import './styles/main.css'

function App() {
  const [token, setToken] = useState(localStorage.getItem('token') || '')
  const [activeTab, setActiveTab] = useState('calculator')

  return (
    <div className="app">
      <header>
        <h1>Calculator Service</h1>
        <nav>
          <button onClick={() => setActiveTab('calculator')}>Calculator</button>
          <button onClick={() => setActiveTab('history')}>History</button>
        </nav>
      </header>

      {!token ? (
        <Auth setToken={setToken} />
      ) : (
        <div className="content">
          {activeTab === 'calculator' && <Calculator token={token} />}
          {activeTab === 'history' && <History token={token} />}
          <button 
            className="logout"
            onClick={() => {
              localStorage.removeItem('token')
              setToken('')
            }}
          >
            Logout
          </button>
        </div>
      )}
    </div>
  )
}

export default App