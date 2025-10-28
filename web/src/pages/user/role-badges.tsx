import { Badge } from '@/components/ui/badge'
import { AVAILABLE_ROLES } from '@/lib/api'

export function RoleBadges({ roles }: { roles: string[] }) {
  return (
    <>
      {roles.map((role) => {
        const cfg = AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]
        return (
          <Badge key={role} variant="outline">
            {cfg?.name ?? role}
          </Badge>
        )
      })}
    </>
  )
}
