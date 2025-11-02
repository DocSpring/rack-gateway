import { z } from './zod'
import { AVAILABLE_ROLES } from './api'

type RoleName = keyof typeof AVAILABLE_ROLES

type NonEmptyRoleTuple = [RoleName, ...RoleName[]]

const roleValues = Object.keys(AVAILABLE_ROLES) as RoleName[]
const roleEnum = z.enum(roleValues as NonEmptyRoleTuple)

const emailMessages = {
  required: 'Email is required',
  format: 'Invalid email format',
  length: 'Email must be 254 characters or less',
} as const

const nameMessages = {
  required: 'Name is required',
  length: 'Name must be 120 characters or less',
} as const

const roleMessages = {
  required: 'At least one role is required',
} as const

const tokenMessages = {
  nameRequired: 'Token name is required',
  nameLength: 'Token name must be 150 characters or less',
  permissionsRequired: 'Select at least one permission',
  permissionLength: 'Permission names must be 150 characters or less',
} as const

export const userFormSchema = z.object({
  email: z
    .string()
    .trim()
    .min(1, emailMessages.required)
    .max(254, emailMessages.length)
    .email(emailMessages.format),
  name: z.string().trim().min(1, nameMessages.required).max(120, nameMessages.length),
  roles: z.array(roleEnum).min(1, roleMessages.required),
})

const permissionSchema = z
  .string()
  .trim()
  .min(1, tokenMessages.permissionsRequired)
  .max(150, tokenMessages.permissionLength)

export const tokenFormSchema = z.object({
  name: z.string().trim().min(1, tokenMessages.nameRequired).max(150, tokenMessages.nameLength),
  permissions: z
    .array(permissionSchema)
    .min(1, tokenMessages.permissionsRequired)
    .transform((permissions) => {
      const unique = Array.from(new Set(permissions))
      return unique.sort()
    }),
})

type ValidationErrors<T extends string> = Partial<Record<T, string>>

export function toFieldErrorMap<TFields extends string>(
  error: z.ZodError,
  fields: readonly TFields[]
): ValidationErrors<TFields> {
  const { fieldErrors } = error.flatten()
  const normalizedFieldErrors = fieldErrors as Record<string, string[]>
  const entries: [TFields, string][] = []
  for (const field of fields) {
    const messages = normalizedFieldErrors[field as string]
    if (messages && messages.length > 0) {
      entries.push([field, messages[0]])
    }
  }
  return Object.fromEntries(entries) as ValidationErrors<TFields>
}
