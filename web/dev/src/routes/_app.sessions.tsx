import { createFileRoute } from '@tanstack/react-router'
import { PanelPlaceholder } from '../components/PanelPlaceholder'

export const Route = createFileRoute('/_app/sessions')({
  component: SessionsPanel,
})

function SessionsPanel() {
  return (
    <PanelPlaceholder
      eyebrow="Management"
      title="Sessions"
      subtitle="Device-level token management: force logout, idle-token pruning and per-user device revocation."
      icon="sessions"
      features={[
        'Stat cards: users, active tokens, used 24h, idle, inactive 30d+',
        'Force logout all users (current session preserved)',
        'Clear inactive tokens with threshold + dry-run preview',
        'Searchable user directory with token counts',
        'Per-device inspection and revocation',
      ]}
    />
  )
}
