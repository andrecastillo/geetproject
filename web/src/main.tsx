import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import App from './App'
import BoardPage from './pages/BoardPage'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<Navigate to="/b" replace />} />
          {/* /b with no slug lands on the first board once boards load. */}
          <Route path="b" element={<BoardPage />} />
          <Route path="b/:slug" element={<BoardPage />} />
          {/* Ticket detail is a real route so it survives a refresh or a paste. */}
          <Route path="b/:slug/t/:key" element={<BoardPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)
