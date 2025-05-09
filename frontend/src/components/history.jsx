import { useState, useEffect } from 'react'

export default function History({ token }) {
  const [expressions, setExpressions] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const fetchHistory = async () => {
      try {
        const response = await fetch('/api/v1/expressions', {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        })

        if (!response.ok) {
          throw new Error('Failed to fetch history')
        }

        const data = await response.json()
        setExpressions(data.expressions)
      } catch (err) {
        setError(err.message)
      } finally {
        setLoading(false)
      }
    }

    fetchHistory()
  }, [token])

  return (
    <div className="history">
      <h2>Calculation History</h2>
      {loading ? (
        <div>Loading...</div>
      ) : error ? (
        <div className="error">Error: {error}</div>
      ) : expressions.length === 0 ? (
        <div>No calculations yet</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Expression</th>
              <th>Status</th>
              <th>Result</th>
            </tr>
          </thead>
          <tbody>
            {expressions.map((expr) => (
              <tr key={expr.id}>
                <td>{expr.expression}</td>
                <td>{expr.status}</td>
                <td>{expr.result || '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}