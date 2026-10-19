import type { ReactNode } from 'react'
import { Card, Icon, PageHeader, type IconName } from './ui'

export function PanelPlaceholder({
  title,
  eyebrow,
  subtitle,
  icon,
  features,
  dependency,
}: {
  title: string
  eyebrow: string
  subtitle: string
  icon: IconName
  features: string[]
  dependency?: ReactNode
}) {
  return (
    <>
      <PageHeader eyebrow={eyebrow} title={title} subtitle={subtitle} />
      <Card>
        <div className="empty">
          <div className="empty-icon">
            <Icon name={icon} />
          </div>
          <div className="empty-title">Panel contract is ready</div>
          <div className="empty-sub">
            The backend contract and UI plan are captured in the Phase 9 spec. This panel lands in a
            following slice of the developer dashboard.
          </div>
          <div className="grid cols-2 mt-16" style={{ width: 'min(720px, 100%)', textAlign: 'left' }}>
            {features.map((feature) => (
              <div key={feature} className="flex small muted">
                <Icon name="check" className="icon-inline" />
                {feature}
              </div>
            ))}
          </div>
          {dependency ? <div className="banner mt-24" style={{ width: 'min(720px, 100%)', textAlign: 'left' }}>{dependency}</div> : null}
        </div>
      </Card>
    </>
  )
}
