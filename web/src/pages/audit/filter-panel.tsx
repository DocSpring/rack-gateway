import { Download, RefreshCw, Search } from 'lucide-react'
import { type ChangeEvent, useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { DateTimePickerField } from '@/pages/audit/date-time-picker-field'
import { ACTION_TYPES, RESOURCE_TYPES, STATUS_TYPES } from '@/pages/audit/utils'

type AuditFilterPanelProps = {
  searchTerm: string
  onSearchChange: (value: string) => void
  actionType: string
  onActionTypeChange: (value: string) => void
  status: string
  onStatusChange: (value: string) => void
  resourceType: string
  onResourceTypeChange: (value: string) => void
  dateRange: string
  onDateRangeChange: (value: string) => void
  perPage: number
  onPerPageChange: (value: number) => void
  isCustomRange: boolean
  customStart: string
  customEnd: string
  onCustomStartChange: (value: string) => void
  onCustomEndChange: (value: string) => void
  onRefresh: () => void
  onExport: () => void
  disableRefresh?: boolean
  disableExport?: boolean
}

const PER_PAGE_OPTIONS = [10, 25, 50, 100, 200]

export function AuditFilterPanel({
  searchTerm,
  onSearchChange,
  actionType,
  onActionTypeChange,
  status,
  onStatusChange,
  resourceType,
  onResourceTypeChange,
  dateRange,
  onDateRangeChange,
  perPage,
  onPerPageChange,
  isCustomRange,
  customStart,
  customEnd,
  onCustomStartChange,
  onCustomEndChange,
  onRefresh,
  onExport,
  disableRefresh = false,
  disableExport = false,
}: AuditFilterPanelProps) {
  const actionOptions = useMemo(() => Object.entries(ACTION_TYPES), [])
  const statusOptions = useMemo(() => Object.entries(STATUS_TYPES), [])
  const resourceOptions = useMemo(() => Object.entries(RESOURCE_TYPES), [])

  const handleSelectChange =
    (handler: (value: string) => void) => (event: ChangeEvent<HTMLSelectElement>) => {
      handler(event.target.value)
    }

  return (
    <Card className="mb-6">
      <CardHeader>
        <CardTitle>Filters</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-4">
          <div className="mx-4 ml-0 flex flex-1 flex-col space-y-2">
            <Label htmlFor="search">Search</Label>
            <div className="relative">
              <Search className="absolute top-2.5 left-2 h-4 w-4 text-muted-foreground" />
              <Input
                className="pl-8"
                id="search"
                onChange={(event) => onSearchChange(event.target.value)}
                placeholder="User, resource, action..."
                value={searchTerm}
              />
            </div>
          </div>

          <AuditSelectField
            id="action-type"
            label="Action Type"
            onChange={handleSelectChange(onActionTypeChange)}
            options={actionOptions}
            value={actionType}
          />

          <AuditSelectField
            id="status"
            label="Status"
            onChange={handleSelectChange(onStatusChange)}
            options={statusOptions}
            value={status}
          />

          <AuditSelectField
            id="resource-type"
            label="Resource Type"
            onChange={handleSelectChange(onResourceTypeChange)}
            options={resourceOptions}
            value={resourceType}
          />

          <div className="mx-4 flex flex-col space-y-2">
            <Label htmlFor="date-range">Date Range</Label>
            <NativeSelect
              className="min-w-[160px]"
              id="date-range"
              onChange={handleSelectChange(onDateRangeChange)}
              value={dateRange}
            >
              <option value="15m">Last 15 Minutes</option>
              <option value="1h">Last Hour</option>
              <option value="24h">Last 24 Hours</option>
              <option value="7d">Last 7 Days</option>
              <option value="30d">Last 30 Days</option>
              <option value="all">All Time</option>
              <option value="custom">Custom…</option>
            </NativeSelect>
          </div>

          <div className="mx-6 mr-0 flex flex-col space-y-2">
            <Label htmlFor="per-page">Per Page</Label>
            <NativeSelect
              className="min-w-[80px]"
              id="per-page"
              onChange={(event) => onPerPageChange(Number(event.target.value))}
              value={String(perPage)}
            >
              {PER_PAGE_OPTIONS.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </NativeSelect>
          </div>
        </div>

        {isCustomRange && (
          <div className="mt-8 flex w-full flex-col gap-12 sm:flex-row">
            <DateTimePickerField
              label="Start"
              maxValue={customEnd}
              onChange={onCustomStartChange}
              value={customStart}
            />
            <DateTimePickerField
              label="End"
              minValue={customStart}
              onChange={onCustomEndChange}
              value={customEnd}
            />
          </div>
        )}

        <div className="mt-8 flex gap-2">
          <Button disabled={disableRefresh} onClick={onRefresh} variant="outline">
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Button disabled={disableExport} onClick={onExport} variant="outline">
            <Download className="mr-2 h-4 w-4" />
            Export CSV
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

type AuditSelectFieldProps = {
  id: string
  label: string
  value: string
  options: [string, string][]
  onChange: (event: ChangeEvent<HTMLSelectElement>) => void
}

function AuditSelectField({ id, label, value, options, onChange }: AuditSelectFieldProps) {
  return (
    <div className="mx-4 flex flex-col space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <NativeSelect className="min-w-[200px]" id={id} onChange={onChange} value={value}>
        {options.map(([optionValue, optionLabel]) => (
          <option key={optionValue} value={optionValue}>
            {optionLabel}
          </option>
        ))}
      </NativeSelect>
    </div>
  )
}
