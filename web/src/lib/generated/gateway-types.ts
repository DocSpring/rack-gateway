/* Do not change, this code is generated from Golang structs */



/* biome-ignore lint -- generated file */
/* biome-ignore format -- generated file */


export interface APIToken {
    id: number;
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
