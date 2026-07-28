import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import App from './App'
import { ALL_SCOPE } from './api'
import BoardPage from './pages/BoardPage'
import { lastScope } from './prefs'
import './styles.css'

function Home() {
  return <Navigate to={`/p/${lastScope(ALL_SCOPE)}`} replace />
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<Home />} />
          {/* A scope with no view named lands on that scope's first view. */}
          <Route path="p/:scope" element={<BoardPage />} />
          <Route path="p/:scope/b/:view" element={<BoardPage />} />
          {/* Ticket detail is a real route so it survives a refresh or a paste. */}
          <Route path="p/:scope/b/:view/t/:key" element={<BoardPage />} />
          <Route path="*" element={<Home />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)
