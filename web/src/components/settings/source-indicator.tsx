import { Copy } from 'lucide-react'
import type { SettingsSetting } from '@/api/schemas'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { toast } from '@/components/ui/use-toast'

export function SourceIndicator({ setting }: { setting: SettingsSetting | undefined }) {
  if (!setting) {
    return null
  }

  const handleCopyEnvVar = () => {
    if (setting.env_var) {
      navigator.clipboard.writeText(setting.env_var)
      // Replace underscores with zero-width space + underscore to allow wrapping
      const wrappableEnvVar = setting.env_var.replace(/_/g, '\u200B_')
      toast.success(`Copied ${wrappableEnvVar} to clipboard`)
    }
  }

  // Always show copy icon when env_var is available
  if (setting.env_var) {
    let label = ''
    let tooltipText = ''

    if (setting.source === 'env') {
      label = `(from env var)`
      tooltipText = `Set via ${setting.env_var} - click to copy`
    } else if (setting.source === 'default') {
      label = '(default)'
      tooltipText = `Set via ${setting.env_var} - click to copy`
    } else if (setting.source === 'db') {
      label = '(database)'
      tooltipText = `Clear to use ${setting.env_var} - click to copy`
    }

    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              className="inline-flex items-center gap-1 text-muted-foreground text-xs hover:text-foreground"
              onClick={handleCopyEnvVar}
              type="button"
            >
              {label}
              <Copy className="size-3" />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{tooltipText}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }

  // Fallback for when there's no env_var (shouldn't happen)
  if (setting.source === 'default') {
    return <span className="text-muted-foreground text-xs">(default)</span>
  }

  return null
}

export function getSettingValue<T>(setting: SettingsSetting | undefined, defaultValue: T): T {
  if (!setting || setting.value === undefined) {
    return defaultValue
  }
  return setting.value as T
}
