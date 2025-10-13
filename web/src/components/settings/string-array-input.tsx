import { Plus, X } from 'lucide-react'
import { useState } from 'react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'

type StringArrayInputProps = {
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  disabled?: boolean
}

export function StringArrayInput({
  value,
  onChange,
  placeholder = 'Enter value',
  disabled = false,
}: StringArrayInputProps) {
  const [newItem, setNewItem] = useState('')

  const handleAddItem = () => {
    if (newItem.trim()) {
      onChange([...value, newItem.trim()])
      setNewItem('')
    }
  }

  const handleRemoveItem = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  return (
    <div className="space-y-2">
      {value.map((item, index) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: Items are simple strings without stable IDs
        <div className="flex items-center gap-2" key={`${item}-${index}`}>
          <code className="flex-1 rounded bg-muted px-3 py-2 font-mono text-sm">{item}</code>
          <Button
            disabled={disabled}
            onClick={() => handleRemoveItem(index)}
            size="sm"
            type="button"
            variant="ghost"
          >
            <X className="size-4" />
          </Button>
        </div>
      ))}
      <div className="flex gap-2">
        <Input
          autoComplete="off"
          disabled={disabled}
          onChange={(e) => setNewItem(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              handleAddItem()
            }
          }}
          placeholder={placeholder}
          type="text"
          value={newItem}
        />
        <Button disabled={disabled || !newItem.trim()} onClick={handleAddItem} type="button">
          <Plus className="size-4" />
        </Button>
      </div>
    </div>
  )
}
