import { useCallback, useState } from 'react'
import { getDefaultLabelForType } from '@/components/account-security/types'
import { toast } from '@/components/ui/use-toast'
import type { MFAStatusResponse } from '@/lib/api'

type MFAMethodRecord = NonNullable<MFAStatusResponse['methods']>[number]

type UseMFAMethodEditResult = {
  editingMethod: { id: number; label: string; type: string; cliCapable: boolean } | null
  editLabel: string
  setEditLabel: (label: string) => void
  editCliCapable: boolean
  setEditCliCapable: (value: boolean) => void
  setEditingMethod: (
    value: { id: number; label: string; type: string; cliCapable: boolean } | null
  ) => void
  handleMethodEdit: (method: MFAMethodRecord) => void
  handleEditDialogCancel: () => void
}

export function useMFAMethodEdit(): UseMFAMethodEditResult {
  const [editingMethod, setEditingMethod] = useState<{
    id: number
    label: string
    type: string
    cliCapable: boolean
  } | null>(null)
  const [editLabel, setEditLabel] = useState('')
  const [editCliCapable, setEditCliCapable] = useState(false)

  const handleMethodEdit = useCallback((method: MFAMethodRecord) => {
    if (!method.id) {
      toast.error('Unable to determine method identifier')
      return
    }
    const label = method.label ?? getDefaultLabelForType(method.type)
    setEditingMethod({
      id: method.id as number,
      label,
      type: method.type,
      cliCapable: method.cli_capable ?? false,
    })
    setEditLabel(label)
    setEditCliCapable(method.cli_capable ?? false)
  }, [])

  const handleEditDialogCancel = useCallback(() => {
    setEditingMethod(null)
    setEditLabel('')
    setEditCliCapable(false)
  }, [])

  return {
    editingMethod,
    editLabel,
    setEditLabel,
    editCliCapable,
    setEditCliCapable,
    setEditingMethod,
    handleMethodEdit,
    handleEditDialogCancel,
  }
}
