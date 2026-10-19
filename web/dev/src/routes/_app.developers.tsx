import { createFileRoute } from '@tanstack/react-router'
import { PanelPlaceholder } from '../components/PanelPlaceholder'

export const Route = createFileRoute('/_app/developers')({
  component: DevelopersPanel,
})

function DevelopersPanel() {
  return (
    <PanelPlaceholder
      eyebrow="Management"
      title="Developers"
      subtitle="Manage holders of the hidden Developer role — the non-super-admin access path to this dashboard."
      icon="developers"
      features={[
        'List users holding the hidden Developer role',
        'Assign / unassign with confirmation',
        'Self-unassign refused (no lockout)',
        'Audited from the Commands log',
      ]}
    />
  )
}
