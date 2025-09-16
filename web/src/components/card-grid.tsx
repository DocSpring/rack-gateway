import { Card, CardContent, CardHeader, CardTitle } from './ui/card'

export type CardItem = {
  label: string
  value?: string | number | null
}

export function CardGrid({ items }: { items: CardItem[] }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3">
      {items.map((it) => (
        <Card key={it.label}>
          <CardHeader className="pb-2">
            <CardTitle className="text-muted-foreground text-sm">{it.label}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="truncate font-medium">{formatValue(it.value)}</div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function formatValue(value?: string | number | null) {
  if (value === null || value === undefined) {
    return '—'
  }
  if (typeof value === 'string' && value.trim() === '') {
    return '—'
  }
  return value
}
