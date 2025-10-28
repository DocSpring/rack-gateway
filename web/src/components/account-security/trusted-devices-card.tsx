import { TimeAgo } from '@/components/time-ago'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import type { MFAStatusResponse } from '@/lib/api'

type TrustedDevice = NonNullable<MFAStatusResponse['trusted_devices']>[number]

type TrustedDevicesCardProps = {
  devices: TrustedDevice[]
  onRevoke: (device: TrustedDevice) => void
  onTrustCurrentDevice: () => void
  trustDevicePending: boolean
  showTrustButton: boolean
}

export function TrustedDevicesCard({
  devices,
  onRevoke,
  onTrustCurrentDevice,
  trustDevicePending,
  showTrustButton,
}: TrustedDevicesCardProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Trusted Devices</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {devices.length === 0 ? (
          <p className="text-muted-foreground text-sm">No trusted devices on file.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[320px] text-left text-sm">
              <thead className="border-b text-muted-foreground text-xs uppercase">
                <tr>
                  <th className="py-2">Device</th>
                  <th className="py-2">Last used</th>
                  <th className="py-2">IP</th>
                  <th className="py-2">Expires</th>
                  <th className="py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {devices.map((device) => (
                  <tr
                    className="border-b last:border-0"
                    key={device.id ?? device.expires_at ?? 'device'}
                  >
                    <td className="py-2">
                      <div className="max-w-[220px] break-words text-xs md:text-sm">
                        {device.label}
                      </div>
                    </td>
                    <td className="py-2">
                      <TimeAgo date={device.last_used_at ?? null} />
                    </td>
                    <td className="py-2 font-mono text-xs md:text-sm">
                      {device.ip_address ?? '—'}
                    </td>
                    <td className="py-2">
                      <TimeAgo date={device.expires_at ?? null} />
                    </td>
                    <td className="py-2 text-right">
                      <Button onClick={() => onRevoke(device)} variant="destructive">
                        Revoke
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
      {showTrustButton ? (
        <CardFooter className="flex flex-wrap gap-2">
          <Button disabled={trustDevicePending} onClick={onTrustCurrentDevice}>
            {trustDevicePending ? 'Trusting…' : 'Trust This Device'}
          </Button>
        </CardFooter>
      ) : null}
    </Card>
  )
}
