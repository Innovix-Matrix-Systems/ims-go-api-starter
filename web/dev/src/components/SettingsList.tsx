import { useEffect, useMemo, useRef, useState } from 'react'
import type { SettingsGroup, SettingsItem } from '../api/types'
import { Select } from './inputs'
import { Button, Card, Chip, EmptyState, Icon, type Tone } from './ui'

/** Unit shown after a value, inferred from the key ("…_days" → days). */
function unitFor(key: string): string | undefined {
  const match = /_(days|hours|minutes|seconds|mb|kb|percent)$/.exec(key)
  if (!match) return undefined
  return match[1] === 'mb' ? 'MB' : match[1] === 'kb' ? 'KB' : match[1] === 'percent' ? '%' : match[1]
}

function humanize(key: string): string {
  const last = key.split('.').pop() ?? key
  return last.replace(/_/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function show(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'boolean') return value ? 'On' : 'Off'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

const typeLabel: Record<SettingsItem['type'], string> = { string: 'string', int: 'integer', bool: 'boolean', json: 'json' }
const sourceTone: Record<SettingsItem['source'], Tone> = { default: 'gray', global: 'orange', tenant: 'blue' }

/**
 * Every setting on one page, grouped, with inline editors (like a Laravel
 * settings screen): switches for booleans, inputs that save on Enter/blur,
 * the code default and a reset link under each control.
 */
export function SettingsList({
  items,
  groups,
  target,
  savingKey,
  onSave,
  onReset,
  onDetails,
}: {
  items: SettingsItem[]
  groups: SettingsGroup[]
  target: string
  savingKey: string | null
  onSave: (item: SettingsItem, value: unknown) => void
  onReset: (item: SettingsItem) => void
  onDetails: (item: SettingsItem) => void
}) {
  const [search, setSearch] = useState('')
  const [group, setGroup] = useState('')
  const [type, setType] = useState('')

  const sections = useMemo(() => {
    const needle = search.trim().toLowerCase()
    const byGroup = new Map<string, { group: SettingsGroup; items: SettingsItem[] }>()
    for (const item of items) {
      if (group && item.group.slug !== group) continue
      if (type && item.type !== type) continue
      if (needle && !`${item.key} ${humanize(item.key)} ${item.description}`.toLowerCase().includes(needle)) continue
      const entry = byGroup.get(item.group.slug) ?? { group: item.group, items: [] }
      entry.items.push(item)
      byGroup.set(item.group.slug, entry)
    }
    return [...byGroup.values()].sort((a, b) => a.group.sort_order - b.group.sort_order || a.group.name.localeCompare(b.group.name))
  }, [items, search, group, type])

  return (
    <div className="mt-24">
      <div className="filters">
        <label className="field grow">
          <span className="field-label">Search</span>
          <input
            className="input"
            placeholder="Key, label or description"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <label className="field" style={{ minWidth: 180 }}>
          <span className="field-label">Group</span>
          <Select
            value={group}
            onChange={setGroup}
            options={[{ value: '', label: 'All groups' }, ...groups.map((item) => ({ value: item.slug, label: item.name }))]}
          />
        </label>
        <label className="field" style={{ minWidth: 150 }}>
          <span className="field-label">Type</span>
          <Select
            value={type}
            onChange={setType}
            options={[
              { value: '', label: 'All types' },
              { value: 'string', label: 'String' },
              { value: 'int', label: 'Integer' },
              { value: 'bool', label: 'Boolean' },
              { value: 'json', label: 'JSON' },
            ]}
          />
        </label>
      </div>
      {sections.length === 0 ? (
        <Card>
          <EmptyState title="No settings match" description="Try another search or filter." icon="settings" />
        </Card>
      ) : (
        <div className="stack">
          {sections.map((section) => (
            <Card key={section.group.slug} tight>
              <div className="setting-group-head">
                <b>{section.group.name}</b>
                <span className="count-pill">{section.items.length}</span>
                {section.group.description ? <span className="small dim">{section.group.description}</span> : null}
              </div>
              {section.items.map((item) => (
                <SettingRow
                  key={item.key}
                  item={item}
                  target={target}
                  saving={savingKey === item.key}
                  onSave={(value) => onSave(item, value)}
                  onReset={() => onReset(item)}
                  onDetails={() => onDetails(item)}
                />
              ))}
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

function SettingRow({
  item,
  target,
  saving,
  onSave,
  onReset,
  onDetails,
}: {
  item: SettingsItem
  target: string
  saving: boolean
  onSave: (value: unknown) => void
  onReset: () => void
  onDetails: () => void
}) {
  const unit = unitFor(item.key)
  const [draft, setDraft] = useState(item.value === null || item.value === undefined ? '' : String(item.value))
  const [error, setError] = useState<string | null>(null)
  // Enter saves, and the blur that follows must not save the same value again.
  const sent = useRef<string | null>(null)
  useEffect(() => {
    sent.current = null
    setDraft(item.value === null || item.value === undefined ? '' : String(item.value))
    setError(null)
  }, [item.value])

  const commit = () => {
    const current = item.value === null || item.value === undefined ? '' : String(item.value)
    if (draft === current || draft === sent.current) return
    if (item.type === 'int') {
      if (!/^-?\d+$/.test(draft.trim())) {
        setError('Enter a whole number')
        return
      }
      sent.current = draft
      onSave(Number.parseInt(draft, 10))
      return
    }
    sent.current = draft
    onSave(draft)
  }

  return (
    <div className={`setting-row ${item.editable ? '' : 'readonly'}`}>
      <div className="setting-info">
        <div className="flex wrap" style={{ gap: 8 }}>
          <b>{humanize(item.key)}</b>
          <span className="mono small dim">{item.key}</span>
          {item.overridden ? <Chip tone={sourceTone[item.source]}>set at {target}</Chip> : null}
          {!item.overridden && item.source !== 'default' ? <Chip tone={sourceTone[item.source]}>from {item.source}</Chip> : null}
        </div>
        <div className="small dim">{item.description}</div>
        <div className="flex wrap mt-8" style={{ gap: 4 }}>
          <span className="tag">{typeLabel[item.type]}</span>
          {unit ? <span className="tag">{unit}</span> : null}
          {item.scope === 'global' ? <span className="tag">global only</span> : null}
        </div>
      </div>
      <div className="setting-control">
        <div className="flex" style={{ gap: 8 }}>
          {item.type === 'bool' ? (
            <button
              type="button"
              role="switch"
              aria-checked={item.value === true}
              aria-label={item.key}
              className={`switch ${item.value === true ? 'on' : ''}`}
              disabled={!item.editable || saving}
              onClick={() => onSave(!(item.value === true))}
            />
          ) : item.type === 'json' ? (
            <button type="button" className="json-preview" onClick={onDetails} disabled={!item.editable}>
              <span className="mono small">{show(item.value)}</span>
              <Icon name="pencil" />
            </button>
          ) : (
            <input
              className={`input ${error ? 'invalid' : ''}`}
              inputMode={item.type === 'int' ? 'numeric' : undefined}
              value={draft}
              disabled={!item.editable || saving}
              aria-label={item.key}
              onChange={(event) => {
                setDraft(event.target.value)
                setError(null)
              }}
              onBlur={commit}
              onKeyDown={(event) => {
                if (event.key === 'Enter') commit()
                if (event.key === 'Escape') setDraft(item.value === null ? '' : String(item.value))
              }}
            />
          )}
          {unit && item.type !== 'bool' ? <span className="small dim">{unit}</span> : null}
          {saving ? <span className="spinner" /> : null}
        </div>
        {error ? <div className="small" style={{ color: 'var(--red)' }}>{error}</div> : null}
        <div className="setting-meta">
          <span>
            Default: <span className="mono">{show(item.default)}</span>
          </span>
          {item.overridden ? (
            <Button size="sm" variant="ghost" onClick={onReset} disabled={saving}>
              Reset
            </Button>
          ) : null}
          <Button size="sm" variant="ghost" onClick={onDetails}>
            Details
          </Button>
        </div>
      </div>
    </div>
  )
}
