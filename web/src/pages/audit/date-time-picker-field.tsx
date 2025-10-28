import { format } from 'date-fns'
import { Calendar as CalendarIcon } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

import { combineDateTime, parseDateTime, splitDateTime } from '@/pages/audit/utils'

type DateTimePickerFieldProps = {
  label: string
  value: string
  onChange: (next: string) => void
  minValue?: string
  maxValue?: string
}

export function DateTimePickerField({
  label,
  value,
  onChange,
  minValue,
  maxValue,
}: DateTimePickerFieldProps) {
  const { datePart, timePart } = useMemo(() => splitDateTime(value), [value])
  const [open, setOpen] = useState(false)
  const selectedDate = useMemo(() => parseDateTime(value), [value])
  const [month, setMonth] = useState<Date>(selectedDate ?? new Date())

  const minParts = useMemo(() => splitDateTime(minValue), [minValue])
  const maxParts = useMemo(() => splitDateTime(maxValue), [maxValue])
  const timeMin = minValue && minParts.datePart === datePart ? minParts.timePart : undefined
  const timeMax = maxValue && maxParts.datePart === datePart ? maxParts.timePart : undefined

  const safeLabel = label.toLowerCase().replace(/\s+/g, '-')
  const dateInputId = `${safeLabel}-date`
  const timeInputId = `${safeLabel}-time`

  return (
    <div className="flex w-full flex-col space-y-2 sm:w-auto sm:min-w-[220px]">
      <Label htmlFor={dateInputId}>{label}</Label>
      <div className="relative">
        <Input
          className="pr-10 font-mono text-sm sm:w-[250px]"
          id={dateInputId}
          onChange={(event) => {
            const nextDate = event.target.value.trim()
            const combined = combineDateTime(nextDate, timePart)
            onChange(combined)
            const parsed = parseDateTime(combined)
            if (parsed) {
              setMonth(parsed)
            }
          }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown') {
              event.preventDefault()
              setOpen(true)
            }
          }}
          placeholder="YYYY-MM-DD"
          value={datePart}
        />
        <Popover onOpenChange={setOpen} open={open}>
          <PopoverTrigger asChild>
            <Button
              className="-translate-y-1/2 absolute top-1/2 right-2 h-8 w-8 p-0 text-muted-foreground hover:bg-transparent focus-visible:ring-1 dark:text-muted-foreground"
              variant="ghost"
            >
              <CalendarIcon className="h-4 w-4" />
              <span className="sr-only">Open calendar for {label}</span>
            </Button>
          </PopoverTrigger>
          <PopoverContent
            align="end"
            alignOffset={-4}
            className="w-auto overflow-hidden p-0"
            sideOffset={8}
          >
            <Calendar
              initialFocus
              mode="single"
              month={month}
              onMonthChange={setMonth}
              onSelect={(selectedDateValue: Date | undefined) => {
                if (!selectedDateValue) {
                  return
                }
                const isoDate = formatDate(selectedDateValue)
                const combined = combineDateTime(isoDate, timePart)
                onChange(combined)
                setOpen(false)
              }}
              selected={selectedDate ?? undefined}
            />
          </PopoverContent>
        </Popover>
      </div>
      <div className="flex items-center gap-2 sm:max-w-[250px]">
        <Label className="sr-only" htmlFor={timeInputId}>
          {label} time
        </Label>
        <Input
          className="bg-background font-mono text-sm sm:w-[120px] dark:bg-background [&::-webkit-calendar-picker-indicator]:hidden dark:[&::-webkit-calendar-picker-indicator]:invert"
          id={timeInputId}
          max={timeMax || undefined}
          min={timeMin || undefined}
          onChange={(event) => {
            const nextTime = event.target.value
            const combined = combineDateTime(datePart, nextTime)
            onChange(combined)
          }}
          step="60"
          type="time"
          value={timePart}
        />
        <span className="text-muted-foreground text-xs">HH:MM</span>
      </div>
    </div>
  )
}

function formatDate(date: Date) {
  return format(date, 'yyyy-MM-dd')
}
