import { createFileRoute } from '@tanstack/react-router'
import { PanelPlaceholder } from '../components/PanelPlaceholder'

export const Route = createFileRoute('/_app/settings')({
  component: SettingsPanel,
})

function SettingsPanel() {
  return (
    <PanelPlaceholder
      eyebrow="Management"
      title="Settings"
      subtitle="Grouped settings board with drag-and-drop assignment and the full resolution chain: user → tenant → global → default."
      icon="settings"
      features={[
        'Settings groups with drag-and-drop and ordering',
        'Edit global and tenant values in place',
        'Per-user overrides with resolution badges',
        'Registry metadata: type, default, scope',
        'settings:sync after catalog changes',
      ]}
      dependency={
        <span>
          <strong>Dependency:</strong> per-user settings targets are being added to the settings
          module; the panel ships global/tenant first and enables the user scope automatically.
        </span>
      }
    />
  )
}
