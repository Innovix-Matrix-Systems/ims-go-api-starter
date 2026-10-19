import { createFileRoute } from '@tanstack/react-router'
import { PanelPlaceholder } from '../components/PanelPlaceholder'

export const Route = createFileRoute('/_app/flags')({
  component: FlagsPanel,
})

function FlagsPanel() {
  return (
    <PanelPlaceholder
      eyebrow="Management"
      title="Feature Flags"
      subtitle="Runtime toggles with global, tenant and user targets — separate from billing entitlements."
      icon="flags"
      features={[
        'Effective state with source badge',
        'Global toggle + tenant/user override drawer',
        'Reset to default and bulk set',
        'feature-flags:sync after registry changes',
        'Audited from the Commands log',
      ]}
    />
  )
}
