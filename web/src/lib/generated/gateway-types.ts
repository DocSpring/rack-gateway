/* Do not change, this code is generated from Golang structs */



/* biome-ignore lint -- generated file */
/* biome-ignore format -- generated file */


export interface APIToken {
    id: number;
    public_id: string;
    name: string;
    user_id: number;
    created_by_user_id?: number;
    created_by_email?: string;
    created_by_name?: string;
    permissions: string[];
    created_at: string;
    expires_at?: string | null;
    last_used_at?: string | null;
}
export interface APITokenResponse {
    token: string;
    api_token?: APIToken;
}
export interface DeployApprovalRequestResponse {
    public_id: string;
    message: string;
    status: string;
    created_at: string;
    updated_at: string;
    created_by_email?: string;
    created_by_name?: string;
    created_by_api_token_id?: string;
    created_by_api_token_name?: string;
    target_api_token_id: string;
    target_api_token_name?: string;
    approved_by_email?: string;
    approved_by_name?: string;
    approved_at?: string;
    approval_expires_at?: string;
    rejected_by_email?: string;
    rejected_by_name?: string;
    rejected_at?: string;
    approval_notes?: string;
    git_commit_hash: string;
    git_branch?: string;
    pr_url?: string;
    ci_metadata?: {[key: string]: any};
    app?: string;
    object_url?: string;
    build_id?: string;
    release_id?: string;
    process_ids?: string[];
    exec_commands?: {[key: string]: any};
    release_created_at?: string;
    release_promoted_at?: string;
    release_promoted_by_api_token_id?: number;
}
