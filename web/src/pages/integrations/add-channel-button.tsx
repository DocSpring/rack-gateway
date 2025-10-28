import { Plus } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

type AddChannelButtonProps = {
  onAdd: (channelName: string) => void
  isUpdating: boolean
}

export function AddChannelButton({ onAdd, isUpdating }: AddChannelButtonProps) {
  const [channelName, setChannelName] = useState('')
  const [isAdding, setIsAdding] = useState(false)

  const handleAdd = () => {
    if (!channelName.trim()) {
      return
    }
    onAdd(channelName)
    setChannelName('')
    setIsAdding(false)
  }

  if (!isAdding) {
    return (
      <Button onClick={() => setIsAdding(true)} variant="outline">
        <Plus className="mr-2 size-4" />
        Add Channel
      </Button>
    )
  }

  return (
    <div className="flex gap-2">
      <Input
        autoFocus
        onChange={(event) => setChannelName(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            handleAdd()
          }
          if (event.key === 'Escape') {
            setIsAdding(false)
            setChannelName('')
          }
        }}
        placeholder="Channel name (e.g., #security)"
        value={channelName}
      />
      <Button disabled={isUpdating || !channelName.trim()} onClick={handleAdd}>
        Add
      </Button>
      <Button
        onClick={() => {
          setIsAdding(false)
          setChannelName('')
        }}
        variant="ghost"
      >
        Cancel
      </Button>
    </div>
  )
}
