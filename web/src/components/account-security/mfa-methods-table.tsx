import { MoreVertical, Pencil, Trash2 } from 'lucide-react'
import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { MFAStatusResponse } from '@/lib/api'
import { getDefaultLabelForType, MFA_METHOD_TYPE_LABELS } from './types'

type MfaMethod = NonNullable<MFAStatusResponse['methods']>[number]

type MfaMethodsTableProps = {
  methods: MfaMethod[]
  onAddMethod: () => void
  addMethodDisabled: boolean
  openDropdownId: number | null
  onDropdownChange: (methodId: number | null) => void
  onEditMethod: (method: MfaMethod) => void
  onRemoveMethod: (method: MfaMethod) => void
}

export function MfaMethodsTable({
  methods,
  onAddMethod,
  addMethodDisabled,
  openDropdownId,
  onDropdownChange,
  onEditMethod,
  onRemoveMethod,
}: MfaMethodsTableProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Registered MFA Methods</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[320px] text-left text-sm">
            <thead className="border-b text-muted-foreground text-xs uppercase">
              <tr>
                <th className="py-2">Type</th>
                <th className="py-2">Label</th>
                <th className="py-2">Added</th>
                <th className="py-2">Last used</th>
                <th className="py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {methods.map((method) => {
                const key =
                  method.id ?? `${method.type ?? 'method'}-${method.created_at ?? 'pending'}`
                return (
                  <tr className="border-b last:border-0" key={key}>
                    <td className="py-2 font-medium">
                      {MFA_METHOD_TYPE_LABELS[(method.type ?? '').toLowerCase()] ??
                        (method.type ? method.type.toUpperCase() : 'MFA')}
                    </td>
                    <td className="py-2">{method.label ?? getDefaultLabelForType(method.type)}</td>
                    <td className="py-2">
                      <TimeAgo date={method.created_at ?? null} />
                    </td>
                    <td className="py-2">
                      {method.last_used_at ? <TimeAgo date={method.last_used_at} /> : 'Never'}
                    </td>
                    <td className="py-2 text-right">
                      <div className="flex justify-end">
                        <DropdownMenu
                          modal={false}
                          onOpenChange={(open) => {
                            onDropdownChange(open ? (method.id as number) : null)
                          }}
                          open={openDropdownId === method.id}
                        >
                          <DropdownMenuTrigger asChild>
                            <Button
                              aria-label={`Actions for ${method.label ?? getDefaultLabelForType(method.type)}`}
                              size="sm"
                              variant="ghost"
                            >
                              <MoreVertical className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent
                            align="end"
                            onCloseAutoFocus={(event) => {
                              event.preventDefault()
                            }}
                          >
                            <DropdownMenuItem onClick={() => onEditMethod(method)}>
                              <Pencil className="h-4 w-4" />
                              Edit
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              onSelect={(event) => {
                                event.preventDefault()
                                const el = document.activeElement
                                if (el instanceof HTMLElement) {
                                  el.setAttribute('inert', '')
                                  el.tabIndex = -1
                                  el.blur()
                                }
                                onDropdownChange(null)
                                requestAnimationFrame(() => {
                                  onRemoveMethod(method)
                                })
                              }}
                              variant="destructive"
                            >
                              <Trash2 className="h-4 w-4" />
                              Remove Method
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </CardContent>
      <CardFooter>
        <Button disabled={addMethodDisabled} onClick={onAddMethod} variant="outline">
          Add Method
        </Button>
      </CardFooter>
    </Card>
  )
}
