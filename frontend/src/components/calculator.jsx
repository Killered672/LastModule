import { useState } from 'react'

export default function Calculator({ token }) {
  const [expression, setExpression] = useState('')
  const [result, setResult] = useState('')
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError('')
    setStatus('Calculating...')

    try {
      const response = await fetch('/api/v1/calculate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ expression })
      })

      if (!response.ok) {
        const errorData = await response.json()
        throw new Error(errorData.error || 'Calculation failed')
      }

      const data = await response.json()
      const expressionId = data.id

      await pollResult(expressionId)
    } catch (err) {
      setError(err.message)
      setStatus('Error')
    }
  }

  const pollResult = async (id) => {
    let attempts = 0
    const maxAttempts = 30

    while (attempts < maxAttempts) {
      try {
        const response = await fetch(`/api/v1/expressions/${id}`, {
          headers: {
            'Authorization': `Bearer ${token}`
          }
        })

        if (!response.ok) {
          throw new Error('Failed to get expression status')
        }

        const data = await response.json()
        const expr = data.expression

        if (expr.status === 'completed') {
          setResult(expr.result)
          setStatus('Completed')
          return
        } else if (expr.status === 'error') {
          setStatus('Error')
          setError('Calculation error')
          return
        }

        attempts++
        await new Promise(resolve => setTimeout(resolve, 1000))
      } catch (err) {
        setError(err.message)
        setStatus('Error')
        return
      }
    }

    setError('Calculation timeout')
    setStatus('Error')
  }

  return (
    <div className="calculator">
      <h2>Calculator</h2>
      <form onSubmit={handleSubmit}>
        <input
          type="text"
          value={expression}
          onChange={(e) => setExpression(e.target.value)}
          placeholder="Enter expression (e.g., 2+2*2)"
          required
        />
        <button type="submit">Calculate</button>
      </form>
      
      {status && <div className="status">Status: {status}</div>}
      {error && <div className="error">Error: {error}</div>}
      {result && <div className="result">Result: {result}</div>}
    </div>
  )
}