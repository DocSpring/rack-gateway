import type { UserEditDialogValues } from '@/components/user-edit-dialog'
import type { GatewayUser, RoleName } from '@/lib/api'

export type EditPlan = {
  originalEmail: string
  routeEmail: string
  trimmedEmail: string
  trimmedName: string
  desiredRoles: RoleName[]
  emailChanged: boolean
  profileChanged: boolean
  shouldUpdateRoles: boolean
}

type EditPlanResult = { plan: EditPlan } | { error: string }

type UpdateProfileVariables = {
  originalEmail: string
  email: string
  name: string
}

type UpdateRoleVariables = {
  email: string
  roles: string[]
}

type EditPlanExecutionDeps = {
  applyProfileUpdate: (
    shouldUpdate: boolean,
    originalEmail: string,
    nextEmail: string,
    nextName: string
  ) => Promise<void>
  applyRoleUpdate: (shouldUpdate: boolean, targetEmail: string, roles: RoleName[]) => Promise<void>
  invalidateUserData: (email: string) => Promise<void>
  invalidateUsersList: () => Promise<void>
  navigateToUser: (email: string) => Promise<void>
}

function buildEditPlan(
  existingUser: GatewayUser,
  routeEmail: string,
  values: UserEditDialogValues
): EditPlanResult {
  const trimmedEmail = values.email.trim()
  const trimmedName = values.name.trim()

  if (!(trimmedEmail && trimmedName)) {
    return { error: 'Email and name are required' }
  }

  const desiredRoles: RoleName[] = [values.role]
  const existingRoles = existingUser.roles ?? []
  const currentEmail = existingUser.email ?? ''
  const currentName = existingUser.name ?? ''
  const emailChanged = trimmedEmail !== currentEmail
  const profileChanged = emailChanged || trimmedName !== currentName
  const rolesChanged =
    existingRoles.length !== desiredRoles.length ||
    desiredRoles.some((role) => !existingRoles.includes(role))

  return {
    plan: {
      originalEmail: currentEmail,
      routeEmail,
      trimmedEmail,
      trimmedName,
      desiredRoles,
      emailChanged,
      profileChanged,
      shouldUpdateRoles: rolesChanged || emailChanged,
    },
  }
}

export async function performProfileUpdate({
  shouldUpdate,
  mutate,
  originalEmail,
  nextEmail,
  nextName,
}: {
  shouldUpdate: boolean
  mutate: (variables: UpdateProfileVariables) => Promise<unknown>
  originalEmail: string
  nextEmail: string
  nextName: string
}): Promise<void> {
  if (!shouldUpdate) {
    return
  }

  await mutate({
    originalEmail,
    email: nextEmail,
    name: nextName,
  })
}

export async function performRoleUpdate({
  shouldUpdate,
  mutate,
  targetEmail,
  roles,
}: {
  shouldUpdate: boolean
  mutate: (variables: UpdateRoleVariables) => Promise<unknown>
  targetEmail: string
  roles: RoleName[]
}): Promise<void> {
  if (!shouldUpdate) {
    return
  }

  await mutate({ email: targetEmail, roles })
}

export async function executeEditPlan(plan: EditPlan, deps: EditPlanExecutionDeps): Promise<void> {
  await deps.applyProfileUpdate(
    plan.profileChanged,
    plan.originalEmail,
    plan.trimmedEmail,
    plan.trimmedName
  )

  await deps.applyRoleUpdate(plan.shouldUpdateRoles, plan.trimmedEmail, plan.desiredRoles)

  const invalidations: Promise<unknown>[] = [
    deps.invalidateUsersList(),
    deps.invalidateUserData(plan.routeEmail),
  ]

  if (plan.emailChanged) {
    invalidations.push(deps.invalidateUserData(plan.trimmedEmail))
  }

  await Promise.all(invalidations)

  if (plan.emailChanged) {
    await deps.navigateToUser(plan.trimmedEmail)
  }
}

type SubmitUserEditsArgs = {
  user?: GatewayUser
  decodedEmail: string
  values: UserEditDialogValues
  toastApi: {
    error: (message: string) => void
    success: (message: string) => void
  }
  executePlan: (plan: EditPlan) => Promise<void>
}

export async function submitUserEdits({
  user,
  decodedEmail,
  values,
  toastApi,
  executePlan,
}: SubmitUserEditsArgs): Promise<void> {
  if (!user) {
    return
  }

  const planResult = buildEditPlan(user, decodedEmail, values)
  if ('error' in planResult) {
    toastApi.error(planResult.error)
    throw new Error(planResult.error)
  }

  try {
    await executePlan(planResult.plan)
    toastApi.success('User updated successfully')
  } catch (error) {
    toastApi.error(error instanceof Error ? error.message : 'Failed to update user')
    throw error
  }
}
