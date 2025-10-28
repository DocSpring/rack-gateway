import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { StringArrayInput } from '@/components/settings/string-array-input'
import { SourceIndicator, getSettingValue } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'

import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { extractErrorMessage } from '@/pages/app-settings/utils'

type StringArrayCardProps = {
	app: string
	settings: AppSettingsResponse | undefined
	disabled: boolean
	settingKey: string
	pathSegment: string
	title: string
	description: string
	placeholder?: string
}

export function StringArrayCard({
	app,
	settings,
	disabled,
	settingKey,
	pathSegment,
	title,
	description,
	placeholder,
}: StringArrayCardProps) {
	const qc = useQueryClient()
	const setting = settings?.[settingKey]
	const currentValue = getSettingValue<string[] | null>(setting, null) ?? []

	const [items, setItems] = useState<string[]>(currentValue)

	const hasChanges =
		items.length !== currentValue.length ||
		items.some((item, index) => item.trim() !== currentValue[index]?.trim())

	const updateMutation = useMutation({
		mutationFn: async () => {
			const filtered = items.map((value) => value.trim()).filter((value) => value.length > 0)
			await api.put(`/api/v1/apps/${app}/settings/${pathSegment}`, filtered)
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ['appSettings', app] })
			toast.success('Setting updated')
		},
		onError: (error: unknown) => {
			const message = extractErrorMessage(error)
			toast.error(message ?? 'Failed to update setting')
		},
	})

	const clearMutation = useMutation({
		mutationFn: async () => {
			await api.delete(`/api/v1/apps/${app}/settings/${pathSegment}`)
		},
		onSuccess: async () => {
			await qc.invalidateQueries({ queryKey: ['appSettings', app] })
			setItems([])
			toast.success('Setting cleared')
		},
		onError: (error: unknown) => {
			const message = extractErrorMessage(error)
			toast.error(message ?? 'Failed to clear setting')
		},
	})

	const handleCancel = () => {
		setItems(currentValue)
	}

	const handleSave = () => {
		updateMutation.mutate()
	}

	const handleClear = () => {
		clearMutation.mutate()
	}

	const hasDbSetting = setting?.source === 'db'

	return (
		<Card>
			<CardHeader>
				<CardTitle>{title}</CardTitle>
			</CardHeader>
			<CardContent className="space-y-4 pb-6">
				<p className="text-muted-foreground text-sm">{description}</p>

				<div className="space-y-2">
					<StringArrayInput
						disabled={disabled}
						onChange={setItems}
						placeholder={placeholder ?? 'Enter value'}
						value={items}
					/>
					<SourceIndicator setting={setting} />
				</div>

				<div className="flex justify-end gap-2">
					{hasDbSetting && !hasChanges && (
						<Button
							disabled={disabled || clearMutation.isPending}
							onClick={handleClear}
							size="sm"
							variant="outline"
						>
							Clear
						</Button>
					)}
					{hasChanges && (
						<>
							<Button disabled={disabled} onClick={handleCancel} size="sm" variant="outline">
								Cancel
							</Button>
							<Button
								disabled={disabled || updateMutation.isPending}
								onClick={handleSave}
								size="sm"
							>
								Save
							</Button>
						</>
					)}
				</div>
			</CardContent>
		</Card>
	)
}
