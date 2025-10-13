import type { ReactNode } from 'react'

export type UserMeta = {
  name?: string | null
  email?: string | null
}

type UserMetaCellProps = UserMeta & {
  fallback?: ReactNode
}

export function UserMetaCell({ name, email, fallback = '—' }: UserMetaCellProps) {
  const displayName = name?.trim()
  const displayEmail = email?.trim()

  if (!(displayName || displayEmail)) {
    if (typeof fallback === 'string') {
      return <span className="text-muted-foreground">{fallback}</span>
    }
    return <>{fallback}</>
  }

  return (
    <div className="flex flex-col text-sm">
      {displayName ? <span className="font-medium">{displayName}</span> : null}
      {displayEmail ? <span className="text-muted-foreground">{displayEmail}</span> : null}
    </div>
  )
}

export function renderUserMeta({ name, email }: UserMeta, fallback?: ReactNode): ReactNode {
  return <UserMetaCell email={email ?? undefined} fallback={fallback} name={name ?? undefined} />
}
