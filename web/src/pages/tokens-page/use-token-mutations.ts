import { useQueryClient } from '@tanstack/react-query'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { QUERY_KEYS } from '@/lib/query-keys'
import { useStepUp } from '../../contexts/step-up-context'
import { api } from '../../lib/api'
import type { APITokenResponse } from '../../lib/generated/gateway-types'

export function useTokenMutations() {
  const queryClient = useQueryClient()
  const { handleStepUpError } = useStepUp()

  const createToken = useMutation({
    mutationFn: async (payload: { name: string; permissions: string[] }) => {
      const response = await api.post<APITokenResponse>('/api/v1/api-tokens', {
        name: payload.name,
        permissions: payload.permissions,
      })
      return response
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.TOKENS })
      toast.success('API token created successfully')
      return data
    },
  })

  const updateToken = useMutation({
    mutationFn: async ({
      publicId,
      name,
      permissions,
    }: {
      publicId: string
      name: string
      permissions: string[]
    }) => {
      await api.put(`/api/v1/api-tokens/${publicId}`, {
        name,
        permissions,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.TOKENS })
      toast.success('Token updated successfully')
    },
  })

  const deleteToken = useMutation({
    mutationFn: async (tokenPublicId: string) => {
      await api.delete(`/api/v1/api-tokens/${tokenPublicId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.TOKENS })
      toast.success('Token deleted successfully')
    },
  })

  return {
    createToken,
    updateToken,
    deleteToken,
    handleStepUpError,
  }
}
