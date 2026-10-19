import { createFileRoute } from '@tanstack/react-router'
import { PanelPlaceholder } from '../components/PanelPlaceholder'

export const Route = createFileRoute('/_app/files')({
  component: FilesPanel,
})

function FilesPanel() {
  return (
    <PanelPlaceholder
      eyebrow="Management"
      title="Files"
      subtitle="File-manager browser over every upload: preview, inspect, download and remove with audit."
      icon="files"
      features={[
        'Grid/list browser with image thumbnails',
        'Search by name, type, disk, owner and uploader',
        'Storage stats by type and disk',
        'Authenticated streaming download for private disks',
        'Audited deletes',
      ]}
    />
  )
}
